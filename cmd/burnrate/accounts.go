package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Verhum/burnrate/internal/config"
	"github.com/Verhum/burnrate/internal/store"
)

func runAccounts() {
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

	cfg, _ := config.Load(st)
	active := cfg.ClaudeConfigDir

	fmt.Println("=== Claude Code accounts ===")
	fmt.Println()
	mark := func(dir string) string {
		if dir == active {
			return "* "
		}
		return "  "
	}
	fmt.Printf("%sinherited environment\n", mark(""))
	for _, a := range config.DiscoverAccounts() {
		kind := "creds: no"
		if a.HasSandboxKeychain {
			kind = "sandbox keychain"
		} else if a.HasCredentialsFile {
			kind = "credentials.json"
		}
		fmt.Printf("%s%s\n", mark(a.ConfigDir), a.Label)
		fmt.Printf("    dir:      %s\n", a.ConfigDir)
		fmt.Printf("    keychain: %s   (%s)\n", a.KeychainSuffix, kind)
	}
	fmt.Println()
	fmt.Println("(* = active) Select via the web UI Config tab, or set")
	fmt.Println("BURNRATE_CLAUDE_CONFIG_DIR for serve/install.")
}
