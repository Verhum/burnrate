package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Verhum/burnrate/internal/ble"
	"github.com/Verhum/burnrate/internal/caffeinate"
	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/daemon"
	"github.com/Verhum/burnrate/internal/git"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/retention"
	"github.com/Verhum/burnrate/internal/scheduler"
	"github.com/Verhum/burnrate/internal/server"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
	"github.com/Verhum/burnrate/internal/whisper"
)

func runServe() {
	git.ScrubEnv()

	dataDir := config.DataDir()
	if err := config.EnsureDirs(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	lock, err := daemon.Acquire(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer lock.Release()

	logger := log.New(filepath.Join(dataDir, "logs", "burnrate.log"), true)

	dbPath := filepath.Join(dataDir, "burnrate.db")
	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	// Every long poll died with the previous daemon, but its `live` flags are
	// still in the database, sorting requests nobody is waiting on to the top
	// of the queue.
	if n, err := st.ClearHumanRequestLive(); err != nil {
		logger.Errorf("clearing stale live request flags: %v", err)
	} else if n > 0 {
		logger.Infof("cleared %d stale live request flag(s) left by a previous daemon", n)
	}

	cfg, err := config.Load(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	client := usage.NewClient(cfg.UsageURL)
	client.ClaudeConfigDir = cfg.ClaudeConfigDir
	client.SandboxKeychain = cfg.SandboxKeychain
	client.SandboxKeychainPasswordFile = cfg.SandboxKeychainPasswordFile

	sched := scheduler.New(st, cfg, client, logger)

	caff := caffeinate.NewManager()
	defer caff.Shutdown()

	whisperSvc := whisper.New(dataDir)
	defer whisperSvc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go whisperSvc.Init(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go sched.Start(ctx)

	bleBridge := ble.New(st, cfg.Port, logger)
	go bleBridge.Start(ctx)

	srv := server.New(st, cfg, sched, caff, whisperSvc, logger)
	// PRs outlive the runs that opened them, so their state is polled rather
	// than recorded once at the end of a run.
	go srv.Prober().Start(ctx)
	// Capture videos are the bulky part of a capture; keyframes and transcripts
	// are kept forever, the videos age out per capture_retention_days.
	go retention.New(st, dataDir, logger).Start(ctx)
	go func() {
		if err := srv.ListenAndServe(ctx); err != nil {
			logger.Errorf("http server error: %v", err)
		}
	}()

	logger.Infof("burnrate serving on 127.0.0.1:%d (data=%s)", cfg.Port, dataDir)

	<-sigCh
	logger.Infof("received signal, shutting down")
	cancel()
	sched.Wait()
}
