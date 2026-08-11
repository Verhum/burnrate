package usage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type credentialsJSON struct {
	ClaudeAiOauth oauthBlob `json:"claudeAiOauth"`
}

type oauthBlob struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
}

type TokenBundle struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64

	Source       string // "keychain" or "file"
	KeychainItem string // service name (keychain) or file path (file)
	KeychainPath string // path to specific keychain db, empty for default
	KeychainPw   string // keychain password (for sandbox unlock on write-back)
	RawJSON      string // full JSON blob for preserving extra fields on write-back
}

var (
	discoveredMu      sync.Mutex
	discoveredService string
)

func Token() (string, error) {
	return TokenForAccount("", "", "")
}

func TokenForAccount(configDir, sandboxKeychain, sandboxKeychainPwFile string) (string, error) {
	if tok := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); tok != "" {
		return tok, nil
	}

	b, err := BundleForAccount(configDir, sandboxKeychain, sandboxKeychainPwFile)
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func BundleForAccount(configDir, sandboxKeychain, sandboxKeychainPwFile string) (*TokenBundle, error) {
	if configDir != "" {
		suffix := ConfigDirSuffix(configDir)
		itemName := "Claude Code-credentials-" + suffix
		if sandboxKeychain != "" {
			if b, err := sandboxKeychainBundle(itemName, sandboxKeychain, sandboxKeychainPwFile); err == nil && b.AccessToken != "" {
				return b, nil
			}
		} else {
			if b, err := keychainBundle(itemName, ""); err == nil && b.AccessToken != "" {
				return b, nil
			}
		}

		if b, err := credentialsFileBundleFrom(configDir); err == nil && b.AccessToken != "" {
			return b, nil
		}
	}

	if b, err := defaultKeychainBundle(); err == nil && b.AccessToken != "" {
		return b, nil
	}

	if b, err := defaultCredentialsFileBundle(); err == nil && b.AccessToken != "" {
		return b, nil
	}

	return nil, fmt.Errorf("no Claude Code OAuth token found (set CLAUDE_CODE_OAUTH_TOKEN, or authenticate via Claude Code)")
}

func ConfigDirSuffix(configDir string) string {
	h := sha256.Sum256([]byte(configDir))
	return fmt.Sprintf("%x", h[:4])
}

func unlockKeychain(keychainPath, pwFile string) (string, error) {
	pw, err := os.ReadFile(pwFile)
	if err != nil {
		return "", fmt.Errorf("read keychain password file: %w", err)
	}
	password := strings.TrimSpace(string(pw))

	unlock := exec.Command("security", "unlock-keychain", "-p", password, keychainPath)
	if out, err := unlock.CombinedOutput(); err != nil {
		return "", fmt.Errorf("unlock keychain: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return password, nil
}

func sandboxKeychainToken(itemName, keychainPath, pwFile string) (string, error) {
	b, err := sandboxKeychainBundle(itemName, keychainPath, pwFile)
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func sandboxKeychainBundle(itemName, keychainPath, pwFile string) (*TokenBundle, error) {
	password, err := unlockKeychain(keychainPath, pwFile)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("security", "find-generic-password", "-s", itemName, "-w", keychainPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	b, err := parseBundle(raw)
	if err != nil {
		return nil, err
	}
	b.Source = "keychain"
	b.KeychainItem = itemName
	b.KeychainPath = keychainPath
	b.KeychainPw = password
	return b, nil
}

func credentialsFileTokenFrom(configDir string) (string, error) {
	b, err := credentialsFileBundleFrom(configDir)
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func credentialsFileBundleFrom(configDir string) (*TokenBundle, error) {
	path := filepath.Join(configDir, ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b, err := parseBundle(string(data))
	if err != nil {
		return nil, err
	}
	b.Source = "file"
	b.KeychainItem = path
	return b, nil
}

func ResetDiscoveredService() {
	discoveredMu.Lock()
	discoveredService = ""
	discoveredMu.Unlock()
}

func keychainToken() (string, error) {
	b, err := defaultKeychainBundle()
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func defaultKeychainBundle() (*TokenBundle, error) {
	discoveredMu.Lock()
	cached := discoveredService
	discoveredMu.Unlock()

	if cached != "" {
		if b, err := keychainBundle(cached, ""); err == nil && b.AccessToken != "" {
			return b, nil
		}
		discoveredMu.Lock()
		discoveredService = ""
		discoveredMu.Unlock()
	}

	if b, err := keychainBundle("Claude Code-credentials", ""); err == nil && b.AccessToken != "" {
		discoveredMu.Lock()
		discoveredService = "Claude Code-credentials"
		discoveredMu.Unlock()
		return b, nil
	}

	names, err := discoverKeychainServices()
	if err != nil || len(names) == 0 {
		return nil, fmt.Errorf("no keychain candidates found")
	}

	for _, name := range names {
		if b, err := keychainBundle(name, ""); err == nil && b.AccessToken != "" {
			discoveredMu.Lock()
			discoveredService = name
			discoveredMu.Unlock()
			return b, nil
		}
	}

	return nil, fmt.Errorf("no valid keychain entry among %d candidates", len(names))
}

func keychainBundle(service, keychainPath string) (*TokenBundle, error) {
	var cmd *exec.Cmd
	if keychainPath != "" {
		cmd = exec.Command("security", "find-generic-password", "-s", service, "-w", keychainPath)
	} else {
		cmd = exec.Command("security", "find-generic-password", "-s", service, "-w")
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	b, err := parseBundle(raw)
	if err != nil {
		return nil, err
	}
	b.Source = "keychain"
	b.KeychainItem = service
	b.KeychainPath = keychainPath
	return b, nil
}

func tryKeychainService(service string) (string, error) {
	b, err := keychainBundle(service, "")
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

var svcNameRe = regexp.MustCompile(`"svce"<blob>="(Claude Code-credentials[^"]*)"`)

func discoverKeychainServices() ([]string, error) {
	cmd := exec.Command("security", "dump-keychain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return extractServiceNames(string(out)), nil
}

func extractServiceNames(dump string) []string {
	matches := svcNameRe.FindAllStringSubmatch(dump, -1)
	seen := make(map[string]bool)
	var names []string
	for _, m := range matches {
		name := m[1]
		if name == "Claude Code-credentials" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func defaultCredentialsFileBundle() (*TokenBundle, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return credentialsFileBundleFrom(filepath.Join(home, ".claude"))
}

func credentialsFileToken() (string, error) {
	b, err := defaultCredentialsFileBundle()
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func parseCredentialsJSON(raw string) (string, error) {
	b, err := parseBundle(raw)
	if err != nil {
		return "", err
	}
	return b.AccessToken, nil
}

func parseBundle(raw string) (*TokenBundle, error) {
	var creds credentialsJSON
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("no access token in credentials")
	}
	return &TokenBundle{
		AccessToken:  creds.ClaudeAiOauth.AccessToken,
		RefreshToken: creds.ClaudeAiOauth.RefreshToken,
		ExpiresAt:    creds.ClaudeAiOauth.ExpiresAt,
		RawJSON:      raw,
	}, nil
}

var acctRe = regexp.MustCompile(`"acct"<blob>="([^"]*)"`)

func keychainAccountAttr(service, keychainPath string) string {
	var cmd *exec.Cmd
	if keychainPath != "" {
		cmd = exec.Command("security", "find-generic-password", "-s", service, keychainPath)
	} else {
		cmd = exec.Command("security", "find-generic-password", "-s", service)
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	m := acctRe.FindSubmatch(out)
	if m == nil {
		return ""
	}
	return string(m[1])
}

func WriteBackKeychain(b *TokenBundle, newJSON string) error {
	if b.Source != "keychain" {
		return fmt.Errorf("cannot write back to source %q", b.Source)
	}

	acct := keychainAccountAttr(b.KeychainItem, b.KeychainPath)

	args := []string{"add-generic-password", "-U", "-s", b.KeychainItem}
	if acct != "" {
		args = append(args, "-a", acct)
	}
	args = append(args, "-w", newJSON)
	if b.KeychainPath != "" {
		if b.KeychainPw != "" {
			unlock := exec.Command("security", "unlock-keychain", "-p", b.KeychainPw, b.KeychainPath)
			if out, err := unlock.CombinedOutput(); err != nil {
				return fmt.Errorf("unlock for write-back: %w (%s)", err, strings.TrimSpace(string(out)))
			}
		}
		args = append(args, b.KeychainPath)
	}

	cmd := exec.Command("security", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write-back: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func WriteBackFile(b *TokenBundle, newJSON string) error {
	if b.Source != "file" {
		return fmt.Errorf("cannot write back to source %q", b.Source)
	}
	return os.WriteFile(b.KeychainItem, []byte(newJSON), 0600)
}

func VerifyWriteBack(b *TokenBundle, expectedAccessToken string) error {
	var raw string
	switch b.Source {
	case "keychain":
		var cmd *exec.Cmd
		if b.KeychainPath != "" {
			cmd = exec.Command("security", "find-generic-password", "-s", b.KeychainItem, "-w", b.KeychainPath)
		} else {
			cmd = exec.Command("security", "find-generic-password", "-s", b.KeychainItem, "-w")
		}
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("verify read-back: %w", err)
		}
		raw = strings.TrimSpace(string(out))
	case "file":
		data, err := os.ReadFile(b.KeychainItem)
		if err != nil {
			return fmt.Errorf("verify read-back: %w", err)
		}
		raw = string(data)
	default:
		return fmt.Errorf("unknown source %q", b.Source)
	}

	readBack, err := parseBundle(raw)
	if err != nil {
		return fmt.Errorf("verify parse: %w", err)
	}
	if readBack.AccessToken != expectedAccessToken {
		return fmt.Errorf("verify mismatch: written token not found on read-back")
	}
	return nil
}
