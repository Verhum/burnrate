package config

import (
	"path/filepath"
	"testing"

	"github.com/Verhum/burnrate/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "burnrate.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// A live UI selection (DB setting) must win over BURNRATE_CLAUDE_CONFIG_DIR,
// which under a launchd install is baked into the plist env. Otherwise the
// dropdown selection would be silently ignored.
func TestLoadAccountDBWinsOverEnv(t *testing.T) {
	st := testStore(t)
	if err := st.SetSetting("claude_config_dir", "/db/selected/.claude"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BURNRATE_CLAUDE_CONFIG_DIR", "/env/plist/.claude")

	cfg, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaudeConfigDir != "/db/selected/.claude" {
		t.Fatalf("DB selection should win, got %q", cfg.ClaudeConfigDir)
	}
}

// Selecting "inherited environment" stores an empty string; that explicit
// choice must still override a plist-baked env var.
func TestLoadAccountInheritedSelectionWinsOverEnv(t *testing.T) {
	st := testStore(t)
	if err := st.SetSetting("claude_config_dir", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BURNRATE_CLAUDE_CONFIG_DIR", "/env/plist/.claude")

	cfg, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaudeConfigDir != "" {
		t.Fatalf("explicit inherited selection should win over env, got %q", cfg.ClaudeConfigDir)
	}
}

// With no DB setting, env acts as the bootstrap default.
func TestLoadAccountEnvBootstrapWhenNoDBSetting(t *testing.T) {
	st := testStore(t)
	t.Setenv("BURNRATE_CLAUDE_CONFIG_DIR", "/env/bootstrap/.claude")

	cfg, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClaudeConfigDir != "/env/bootstrap/.claude" {
		t.Fatalf("env should be bootstrap default, got %q", cfg.ClaudeConfigDir)
	}
}

// max_auto_continue follows the normal precedence: default -> DB setting -> env.
// 0 is a meaningful value (auto-continue disabled), so it must survive.
func TestLoadMaxAutoContinue(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg, err := Load(testStore(t))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxAutoContinue != 3 {
			t.Fatalf("MaxAutoContinue = %d, want 3", cfg.MaxAutoContinue)
		}
	})

	t.Run("db setting", func(t *testing.T) {
		st := testStore(t)
		if err := st.SetSetting("max_auto_continue", "0"); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(st)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxAutoContinue != 0 {
			t.Fatalf("MaxAutoContinue = %d, want 0 (disabled)", cfg.MaxAutoContinue)
		}
	})

	t.Run("env wins", func(t *testing.T) {
		st := testStore(t)
		if err := st.SetSetting("max_auto_continue", "2"); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BURNRATE_MAX_AUTO_CONTINUE", "7")
		cfg, err := Load(st)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxAutoContinue != 7 {
			t.Fatalf("MaxAutoContinue = %d, want 7", cfg.MaxAutoContinue)
		}
	})
}
