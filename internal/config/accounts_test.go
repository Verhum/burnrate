package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirSuffixKnownVector(t *testing.T) {
	// Must stay in sync with usage.ConfigDirSuffix.
	got := configDirSuffix("/Users/example/code/demo/.local_home/.claude")
	if got != "d9455aca" {
		t.Fatalf("expected d9455aca, got %s", got)
	}
}

func TestDiscoverAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Default location.
	mkdir(t, filepath.Join(home, ".claude"))
	writeFile(t, filepath.Join(home, ".claude", ".credentials.json"), "{}")

	// Project sandbox WITH keychain + password file.
	p1 := filepath.Join(home, "code", "humansonly", ".local_home")
	mkdir(t, filepath.Join(p1, ".claude"))
	mkdir(t, filepath.Join(p1, "Library", "Keychains"))
	writeFile(t, filepath.Join(p1, "Library", "Keychains", "sandbox.keychain-db"), "x")
	writeFile(t, filepath.Join(p1, ".keychain-password"), "pw")

	// Project sandbox WITHOUT the sandbox keychain files.
	p2 := filepath.Join(home, "code", "other", ".local_home")
	mkdir(t, filepath.Join(p2, ".claude"))

	accts := DiscoverAccounts()
	if len(accts) != 3 {
		t.Fatalf("expected 3 accounts, got %d: %+v", len(accts), accts)
	}

	byLabel := map[string]Account{}
	for _, a := range accts {
		if len(a.KeychainSuffix) != 8 {
			t.Errorf("account %q: suffix not 8 hex chars: %q", a.Label, a.KeychainSuffix)
		}
		byLabel[a.Label] = a
	}

	def, ok := byLabel["default (~/.claude)"]
	if !ok || def.Source != "default" || !def.HasCredentialsFile {
		t.Errorf("default account wrong: %+v", def)
	}

	h, ok := byLabel["humansonly"]
	if !ok || h.Source != "project" || !h.HasSandboxKeychain {
		t.Errorf("humansonly sandbox account wrong: %+v", h)
	}
	if h.SandboxKeychain == "" || h.SandboxPasswordFile == "" {
		t.Errorf("humansonly should carry sandbox paths: %+v", h)
	}

	o, ok := byLabel["other"]
	if !ok || o.HasSandboxKeychain {
		t.Errorf("other account should have no sandbox keychain: %+v", o)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
