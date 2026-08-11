package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/usage"
)

// runToken prints a fresh OAuth token for the configured account and nothing
// else, so it can be consumed as `CLAUDE_CODE_OAUTH_TOKEN="$(burnrate token)"`.
//
// It exists because a pinned account keeps its credentials in a sandbox keychain
// that `claude` cannot read on its own — the daemon already resolves exactly this
// token for every agent it spawns (runner.resolveTokenEnv), and a human resuming
// one of those sessions by hand needs the same thing. Nothing is printed for an
// inherited-environment account: there the CLI finds its own credentials.
func runToken() {
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
	if cfg.ClaudeConfigDir == "" {
		fmt.Fprintln(os.Stderr, "no pinned account: claude finds its own credentials")
		os.Exit(1)
	}

	b, err := usage.BundleForAccount(cfg.ClaudeConfigDir, cfg.SandboxKeychain, cfg.SandboxKeychainPasswordFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving token bundle: %v\n", err)
		os.Exit(1)
	}
	tok, _, err := usage.EnsureFresh(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error refreshing token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}
