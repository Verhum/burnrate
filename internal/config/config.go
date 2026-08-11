package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/store"
)

type SizeEstimate struct {
	BudgetUSD  int           `json:"budget_usd"`
	MaxTimeout time.Duration `json:"max_timeout"`
}

type Config struct {
	ParallelN         int
	UtilThreshold     float64
	SevenDayThreshold float64
	Model             string
	PollIntervalSec   int
	MaxAttempts       int
	MaxAutoContinue   int
	BaseCodeDir       string
	WorktreeRoot      string
	Port              int
	UsageURL          string
	DryRun            bool
	DataDir           string
	SizeEstimates     map[string]SizeEstimate

	ClaudeConfigDir             string
	SandboxKeychain             string
	SandboxKeychainPasswordFile string
}

var defaultEstimates = map[string]SizeEstimate{
	"medium": {BudgetUSD: 15, MaxTimeout: 75 * time.Minute},
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func DataDir() string {
	if d := os.Getenv("BURNRATE_DATA_DIR"); d != "" {
		return expandHome(d)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".burnrate")
}

// SelfExe is the absolute path to the running burnrate binary, for building
// commands that call back into it. Falls back to the bare name, which needs
// burnrate on PATH — a wrong-but-readable command beats an empty one.
func SelfExe() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return "burnrate"
}

// AgentWorkRoot holds one working directory per agent-directed run. Unlike
// WorktreeRoot these are plain directories: the agent adds its own worktrees
// inside, one per repo it decides the task touches.
func (c Config) AgentWorkRoot() string {
	return filepath.Join(c.DataDir, "agentwork")
}

func EnsureDirs(dataDir string) error {
	for _, sub := range []string{"", "logs", "worktrees", "agentwork", "attachments", "captures", "mcp"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	return nil
}

func Load(st *store.Store) (Config, error) {
	dataDir := DataDir()
	cfg := Config{
		ParallelN:         6,
		UtilThreshold:     100,
		SevenDayThreshold: 95,
		Model:             "claude-opus-4-6",
		PollIntervalSec:   5,
		// Attempts count interruptions, not just failures: a task suspended when
		// the session limit is spent resumes as the next attempt. Sized for that
		// — ~20 session rollovers is roughly four days of continuously
		// interrupted work — rather than for a count of genuine failures.
		MaxAttempts:     20,
		MaxAutoContinue: 3,
		BaseCodeDir:     expandHome("~/code"),
		WorktreeRoot:    filepath.Join(dataDir, "worktrees"),
		Port:            9112,
		UsageURL:        "https://api.anthropic.com/api/oauth/usage",
		DryRun:          false,
		DataDir:         dataDir,
		SizeEstimates:   copyEstimates(defaultEstimates),
	}

	// Account keys use inverted precedence: a DB setting (set via the web UI /
	// `POST /api/accounts/select`) wins over the environment, so a live selection
	// is not silently overridden by BURNRATE_CLAUDE_CONFIG_DIR baked into the
	// launchd plist. dbAccount records which account keys the DB supplied so
	// applyEnv can treat env as a bootstrap default for those keys only.
	var dbAccount map[string]bool
	if st != nil {
		dbAccount = applySettings(st, &cfg)
	}
	applyEnv(&cfg, dbAccount)
	autoDeriveSandbox(&cfg)

	return cfg, nil
}

func applySettings(st *store.Store, cfg *Config) map[string]bool {
	dbAccount := map[string]bool{}
	if v, ok := st.GetSetting("parallel_n"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ParallelN = n
		}
	}
	if v, ok := st.GetSetting("util_threshold"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.UtilThreshold = f
		}
	}
	if v, ok := st.GetSetting("sevenday_threshold"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SevenDayThreshold = f
		}
	}
	if v, ok := st.GetSetting("model"); ok {
		cfg.Model = v
	}
	if v, ok := st.GetSetting("poll_interval_sec"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PollIntervalSec = n
		}
	}
	if v, ok := st.GetSetting("max_attempts"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxAttempts = n
		}
	}
	if v, ok := st.GetSetting("max_auto_continue"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxAutoContinue = n
		}
	}
	if v, ok := st.GetSetting("base_code_dir"); ok {
		cfg.BaseCodeDir = expandHome(v)
	}
	if v, ok := st.GetSetting("worktree_root"); ok {
		cfg.WorktreeRoot = expandHome(v)
	}
	if v, ok := st.GetSetting("port"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v, ok := st.GetSetting("usage_url"); ok {
		cfg.UsageURL = v
	}
	if v, ok := st.GetSetting("dry_run"); ok {
		cfg.DryRun = v == "true" || v == "1"
	}

	// Presence (ok), not a non-empty value, marks a key as DB-supplied: selecting
	// the "inherited environment" account stores an empty string, and that choice
	// must still win over a plist-baked env var.
	if v, ok := st.GetSetting("claude_config_dir"); ok {
		cfg.ClaudeConfigDir = v
		dbAccount["claude_config_dir"] = true
	}
	if v, ok := st.GetSetting("sandbox_keychain"); ok {
		cfg.SandboxKeychain = v
		dbAccount["sandbox_keychain"] = true
	}
	if v, ok := st.GetSetting("sandbox_keychain_password_file"); ok {
		cfg.SandboxKeychainPasswordFile = v
		dbAccount["sandbox_keychain_password_file"] = true
	}

	return dbAccount
}

func applyEnv(cfg *Config, dbAccount map[string]bool) {
	if v := os.Getenv("BURNRATE_PARALLEL_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ParallelN = n
		}
	}
	if v := os.Getenv("BURNRATE_UTIL_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.UtilThreshold = f
		}
	}
	if v := os.Getenv("BURNRATE_SEVENDAY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SevenDayThreshold = f
		}
	}
	if v := os.Getenv("BURNRATE_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("BURNRATE_POLL_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PollIntervalSec = n
		}
	}
	if v := os.Getenv("BURNRATE_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxAttempts = n
		}
	}
	if v := os.Getenv("BURNRATE_MAX_AUTO_CONTINUE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxAutoContinue = n
		}
	}
	if v := os.Getenv("BURNRATE_BASE_CODE_DIR"); v != "" {
		cfg.BaseCodeDir = expandHome(v)
	}
	if v := os.Getenv("BURNRATE_WORKTREE_ROOT"); v != "" {
		cfg.WorktreeRoot = expandHome(v)
	}
	if v := os.Getenv("BURNRATE_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("BURNRATE_USAGE_URL"); v != "" {
		cfg.UsageURL = v
	}
	if v := os.Getenv("BURNRATE_DRYRUN"); v != "" {
		cfg.DryRun = v == "1" || v == "true"
	}
	// Account keys: env is only a bootstrap default. If the DB supplied the key
	// (a live UI selection), the env value is ignored so the selection sticks.
	if v := os.Getenv("BURNRATE_CLAUDE_CONFIG_DIR"); v != "" && !dbAccount["claude_config_dir"] {
		cfg.ClaudeConfigDir = v
	}
	if v := os.Getenv("BURNRATE_SANDBOX_KEYCHAIN"); v != "" && !dbAccount["sandbox_keychain"] {
		cfg.SandboxKeychain = v
	}
	if v := os.Getenv("BURNRATE_SANDBOX_KEYCHAIN_PASSWORD_FILE"); v != "" && !dbAccount["sandbox_keychain_password_file"] {
		cfg.SandboxKeychainPasswordFile = v
	}
}

func autoDeriveSandbox(cfg *Config) {
	if cfg.ClaudeConfigDir == "" || cfg.SandboxKeychain != "" {
		return
	}
	parent := filepath.Dir(cfg.ClaudeConfigDir)
	kc := filepath.Join(parent, "Library", "Keychains", "sandbox.keychain-db")
	pw := filepath.Join(parent, ".keychain-password")
	if fileExists(kc) && fileExists(pw) {
		cfg.SandboxKeychain = kc
		cfg.SandboxKeychainPasswordFile = pw
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyEstimates(src map[string]SizeEstimate) map[string]SizeEstimate {
	dst := make(map[string]SizeEstimate, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
