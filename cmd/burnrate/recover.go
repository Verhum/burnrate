package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/recovery"
	"github.com/Verhum/burnrate/internal/store"
)

func runRecover() {
	dataDir := config.DataDir()
	if err := config.EnsureDirs(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	logger := log.New(filepath.Join(dataDir, "logs", "burnrate.log"), true)

	dbPath := filepath.Join(dataDir, "burnrate.db")
	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	cfg, err := config.Load(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	fmt.Println("Running recovery sweep...")
	if err := recovery.Sweep(ctx, st, cfg, logger); err != nil {
		fmt.Fprintf(os.Stderr, "sweep error: %v\n", err)
	}

	fmt.Println("Cleaning up stale worktrees...")
	if err := recovery.CleanupStale(ctx, st, cfg, logger); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup error: %v\n", err)
	}

	fmt.Println("Recovery complete.")
}
