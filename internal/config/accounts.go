package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Account describes a Claude Code account that burnrate can be pinned to.
// ConfigDir == "" is the special "inherited environment" case and is not
// produced by discovery (the API layer adds it).
type Account struct {
	ConfigDir           string `json:"config_dir"`
	Label               string `json:"label"`
	Source              string `json:"source"` // "default" | "project"
	KeychainSuffix      string `json:"keychain_suffix,omitempty"`
	SandboxKeychain     string `json:"sandbox_keychain,omitempty"`
	SandboxPasswordFile string `json:"sandbox_password_file,omitempty"`
	HasSandboxKeychain  bool   `json:"has_sandbox_keychain"`
	HasCredentialsFile  bool   `json:"has_credentials_file"`
}

// accountCodeRoot is the directory scanned for project-local sandboxed accounts
// (each at `<root>/*/.local_home/.claude`, alongside a sibling sandbox keychain).
func accountCodeRoot() string { return expandHome("~/code") }

// DiscoverAccounts scans the default config location (~/.claude) and every
// project-local sandbox under ~/code/*/.local_home/.claude, returning the ones
// that exist. Read-only: it never unlocks keychains or reads credentials.
func DiscoverAccounts() []Account {
	var accounts []Account
	seen := map[string]bool{}

	add := func(a Account) {
		if a.ConfigDir == "" || seen[a.ConfigDir] {
			return
		}
		seen[a.ConfigDir] = true
		a.KeychainSuffix = configDirSuffix(a.ConfigDir)
		if _, err := os.Stat(filepath.Join(a.ConfigDir, ".credentials.json")); err == nil {
			a.HasCredentialsFile = true
		}
		accounts = append(accounts, a)
	}

	if def := expandHome("~/.claude"); isDir(def) {
		add(Account{ConfigDir: def, Label: "default (~/.claude)", Source: "default"})
	}

	root := accountCodeRoot()
	matches, _ := filepath.Glob(filepath.Join(root, "*", ".local_home", ".claude"))
	sort.Strings(matches)
	for _, dir := range matches {
		if !isDir(dir) {
			continue
		}
		proj := dir
		if rel, err := filepath.Rel(root, dir); err == nil {
			if parts := strings.Split(rel, string(filepath.Separator)); len(parts) > 0 {
				proj = parts[0]
			}
		}
		a := Account{ConfigDir: dir, Label: proj, Source: "project"}
		sandboxHome := filepath.Dir(dir) // <proj>/.local_home
		kc := filepath.Join(sandboxHome, "Library", "Keychains", "sandbox.keychain-db")
		pw := filepath.Join(sandboxHome, ".keychain-password")
		if fileExists(kc) && fileExists(pw) {
			a.HasSandboxKeychain = true
			a.SandboxKeychain = kc
			a.SandboxPasswordFile = pw
		}
		add(a)
	}

	return accounts
}

// configDirSuffix mirrors usage.ConfigDirSuffix (sha256(dir)[:8] hex). Kept here
// so the config package stays free of a dependency on internal/usage.
func configDirSuffix(configDir string) string {
	h := sha256.Sum256([]byte(configDir))
	return fmt.Sprintf("%x", h[:4])
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
