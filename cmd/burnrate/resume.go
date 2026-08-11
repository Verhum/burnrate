package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/store"
)

// runResume prints the shell command that reattaches a terminal to a run's
// Claude session. Printing rather than exec'ing keeps it composable —
// `eval "$(burnrate resume 42)"` — and lets the user see which worktree and
// account the session belongs to before jumping into it.
func runResume(args []string) {
	dataDir := config.DataDir()
	if err := config.EnsureDirs(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	st, err := store.Open(filepath.Join(dataDir, "burnrate.db"))
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

	if len(args) == 0 {
		listResumable(st, cfg.ClaudeConfigDir)
		return
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage: burnrate resume [run-id]\n")
		os.Exit(1)
	}

	run, err := st.GetRun(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: no run %d\n", id)
		os.Exit(1)
	}
	cmd := claude.ResumeCommand(resumeTarget(run.SessionID, run.WorktreePath, cfg.ClaudeConfigDir))
	if cmd == "" {
		fmt.Fprintf(os.Stderr, "run %d recorded no session id, so it cannot be resumed\n", id)
		os.Exit(1)
	}
	fmt.Println(cmd)
}

func resumeTarget(sessionID, workDir, configDir string) claude.ResumeTarget {
	return claude.ResumeTarget{
		SessionID:    sessionID,
		WorkDir:      workDir,
		ConfigDir:    configDir,
		TokenCommand: config.SelfExe() + " token",
	}
}

func listResumable(st *store.Store, configDir string) {
	runs, err := st.ListRuns(0, 50)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing runs: %v\n", err)
		os.Exit(1)
	}
	shown := 0
	for _, r := range runs {
		cmd := claude.ResumeCommand(resumeTarget(r.SessionID, r.WorktreePath, configDir))
		if cmd == "" {
			continue
		}
		fmt.Printf("run %d  task %d  %s  %s\n", r.ID, r.TaskID, r.Status, formatStartedAt(r.StartedAt, time.Now()))
		fmt.Printf("  %s\n", cmd)
		shown++
	}
	if shown == 0 {
		fmt.Println("No runs with a recorded session.")
		return
	}
	fmt.Println()
	fmt.Println("Run `burnrate resume <run-id>` for just the command.")
}
