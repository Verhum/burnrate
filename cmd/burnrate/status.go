package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

func runStatus() {
	dataDir := config.DataDir()
	if err := config.EnsureDirs(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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

	fmt.Println("=== burnrate status ===")
	fmt.Println()

	if cfg.ClaudeConfigDir != "" {
		suffix := usage.ConfigDirSuffix(cfg.ClaudeConfigDir)
		fmt.Printf("Account: %s (keychain %s)\n", cfg.ClaudeConfigDir, suffix)
		printTokenFreshness(cfg)
	} else {
		fmt.Println("Account: inherited environment")
	}
	fmt.Println()

	client := usage.NewClient(cfg.UsageURL)
	client.ClaudeConfigDir = cfg.ClaudeConfigDir
	client.SandboxKeychain = cfg.SandboxKeychain
	client.SandboxKeychainPasswordFile = cfg.SandboxKeychainPasswordFile
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap, err := client.Fetch(ctx)
	if err != nil {
		fmt.Printf("Usage: error fetching (%v)\n", err)
	} else {
		fmt.Printf("5-hour:  %.1f%% utilization", snap.FiveHour.Utilization)
		if !snap.FiveHour.ResetsAt.IsZero() {
			delta := time.Until(snap.FiveHour.ResetsAt)
			fmt.Printf("  (resets %s, %s from now)",
				snap.FiveHour.ResetsAt.Local().Format("15:04"), formatDuration(delta))
		}
		fmt.Println()

		fmt.Printf("7-day:   %.1f%% utilization", snap.SevenDay.Utilization)
		if !snap.SevenDay.ResetsAt.IsZero() {
			delta := time.Until(snap.SevenDay.ResetsAt)
			fmt.Printf("  (resets %s)", formatDuration(delta))
		}
		fmt.Println()

		for _, sw := range snap.ScopedWeekly {
			fmt.Printf("  %s week: %.0f%%\n", sw.Model, sw.Percent)
		}
	}

	fmt.Println()

	counts, err := st.TaskCountsByStatus()
	if err != nil {
		fmt.Printf("Tasks: error (%v)\n", err)
	} else if len(counts) == 0 {
		fmt.Println("Tasks: none")
	} else {
		fmt.Print("Tasks:")
		for _, status := range []string{"queued", "running", "resumable", "done", "failed", "paused"} {
			if c, ok := counts[status]; ok {
				fmt.Printf("  %s=%d", status, c)
			}
		}
		fmt.Println()
	}

	runs, err := st.ListRuns(0, 3)
	if err != nil {
		fmt.Printf("Runs: error (%v)\n", err)
	} else if len(runs) == 0 {
		fmt.Println("Runs: none")
	} else {
		fmt.Println()
		fmt.Println("Latest runs:")
		now := time.Now()
		for _, r := range runs {
			line := fmt.Sprintf("  #%d task=%d status=%s attempt=%d started=%s",
				r.ID, r.TaskID, r.Status, r.Attempt, formatStartedAt(r.StartedAt, now))
			if r.PRURL != "" {
				line += " pr=" + r.PRURL
			}
			if r.Error != "" {
				line += " err=" + r.Error
			}
			fmt.Println(line)
		}
	}
}

func printTokenFreshness(cfg config.Config) {
	b, err := usage.BundleForAccount(cfg.ClaudeConfigDir, cfg.SandboxKeychain, cfg.SandboxKeychainPasswordFile)
	if err != nil {
		fmt.Printf("Token:   error (%v)\n", err)
		return
	}
	exp := usage.ExpiresTime(b)
	if exp.IsZero() {
		fmt.Println("Token:   valid (no expiry info)")
		return
	}
	remaining := time.Until(exp)
	if remaining <= 0 {
		fmt.Println("Token:   expired (will refresh on next use)")
	} else {
		fmt.Printf("Token:   valid %s\n", formatDuration(remaining))
	}
}

// formatStartedAt renders a run's stored start time (UTC RFC3339) as a local
// wall-clock label plus how long ago it was. The clock time is what lines a run
// up against a session window or a `run-<id>.jsonl` timestamp; the elapsed part
// is what says whether it is still fresh. Today's runs drop the date, since the
// list is almost always about today.
func formatStartedAt(iso string, now time.Time) string {
	if iso == "" {
		return "unknown"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	local := t.In(now.Location())
	label := local.Format("Jan 2 15:04")
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		label = local.Format("15:04")
	}
	d := now.Sub(local)
	switch {
	case d < 0:
		return label
	case d < time.Minute:
		return label + " (just now)"
	default:
		return fmt.Sprintf("%s (%s ago)", label, formatAgo(d))
	}
}

// formatAgo is formatDuration with a day bucket, since a run list reaches back
// further than a reset countdown does: last week's run reads "9d 0h ago" rather
// than "216h10m ago".
func formatAgo(d time.Duration) string {
	if d >= 48*time.Hour {
		h := int(d.Hours())
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	return formatDuration(d)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "overdue"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
