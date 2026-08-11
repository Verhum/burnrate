package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCredentialsJSON(t *testing.T) {
	raw := `{"claudeAiOauth":{"accessToken":"abc123"}}`
	tok, err := parseCredentialsJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tok != "abc123" {
		t.Fatalf("expected abc123, got %s", tok)
	}
}

func TestParseCredentialsJSONMissingToken(t *testing.T) {
	raw := `{"claudeAiOauth":{}}`
	_, err := parseCredentialsJSON(raw)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestParseCredentialsJSONInvalid(t *testing.T) {
	_, err := parseCredentialsJSON("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseCredentialsJSONNested(t *testing.T) {
	raw := `{
		"claudeAiOauth": {
			"accessToken": "tok-456",
			"refreshToken": "ref-789",
			"expiresAt": 1735689600000
		},
		"otherField": "value"
	}`
	tok, err := parseCredentialsJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tok != "tok-456" {
		t.Fatalf("expected tok-456, got %s", tok)
	}
}

func TestExtractServiceNames(t *testing.T) {
	dump := `keychain: "/Users/test/Library/Keychains/login.keychain-db"
version: 512
class: "genp"
    0x00000007 <blob>="Claude Code-credentials"
    "svce"<blob>="Claude Code-credentials"
class: "genp"
    0x00000007 <blob>="Claude Code-credentials-fe572ecd"
    "svce"<blob>="Claude Code-credentials-fe572ecd"
class: "genp"
    0x00000007 <blob>="Claude Code-credentials-ab123456"
    "svce"<blob>="Claude Code-credentials-ab123456"
class: "genp"
    0x00000007 <blob>="SomeOtherApp"
    "svce"<blob>="SomeOtherApp"
`

	names := extractServiceNames(dump)
	if len(names) != 2 {
		t.Fatalf("expected 2 suffixed names, got %d: %v", len(names), names)
	}
	if names[0] != "Claude Code-credentials-ab123456" {
		t.Errorf("names[0] = %q, want Claude Code-credentials-ab123456", names[0])
	}
	if names[1] != "Claude Code-credentials-fe572ecd" {
		t.Errorf("names[1] = %q, want Claude Code-credentials-fe572ecd", names[1])
	}
}

func TestExtractServiceNamesEmpty(t *testing.T) {
	names := extractServiceNames(`keychain: "/Users/test/Library/Keychains/login.keychain-db"
version: 512
class: "genp"
    "svce"<blob>="SomeOtherApp"
`)
	if len(names) != 0 {
		t.Fatalf("expected 0 names, got %d: %v", len(names), names)
	}
}

func TestExtractServiceNamesOnlyLegacy(t *testing.T) {
	names := extractServiceNames(`class: "genp"
    "svce"<blob>="Claude Code-credentials"
`)
	if len(names) != 0 {
		t.Fatalf("expected 0 names (legacy excluded), got %d: %v", len(names), names)
	}
}

func TestExtractServiceNamesDuplicates(t *testing.T) {
	dump := `    "svce"<blob>="Claude Code-credentials-fe572ecd"
    "svce"<blob>="Claude Code-credentials-fe572ecd"
`
	names := extractServiceNames(dump)
	if len(names) != 1 {
		t.Fatalf("expected 1 deduplicated name, got %d: %v", len(names), names)
	}
}

func TestConfigDirSuffix(t *testing.T) {
	got := ConfigDirSuffix("/Users/example/code/demo/.local_home/.claude")
	if got != "d9455aca" {
		t.Fatalf("expected d9455aca, got %s", got)
	}
}

func TestConfigDirSuffixDifferentDir(t *testing.T) {
	a := ConfigDirSuffix("/home/alice/.claude")
	b := ConfigDirSuffix("/home/bob/.claude")
	if a == b {
		t.Fatal("different config dirs should produce different suffixes")
	}
	if len(a) != 8 || len(b) != 8 {
		t.Fatalf("suffixes should be 8 hex chars, got %q and %q", a, b)
	}
}

func TestTokenForAccountPrefersKeychainViaFakeSecurity(t *testing.T) {
	configDir := "/test/config/.claude"
	suffix := ConfigDirSuffix(configDir)
	expectedItem := "Claude Code-credentials-" + suffix

	dir := t.TempDir()
	fakeScript := filepath.Join(dir, "security")
	creds := `{"claudeAiOauth":{"accessToken":"pinned-token-123"}}`
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"find-generic-password\" ] && echo \"$@\" | grep -q '" + expectedItem + "'; then\n" +
		"  echo '" + creds + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(fakeScript, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	ResetDiscoveredService()

	tok, err := TokenForAccount(configDir, "", "")
	if err != nil {
		t.Fatalf("TokenForAccount: %v", err)
	}
	if tok != "pinned-token-123" {
		t.Fatalf("expected pinned-token-123, got %s", tok)
	}
}

func TestTokenForAccountCredentialsFileFallback(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	creds := `{"claudeAiOauth":{"accessToken":"file-token-456"}}`
	if err := os.WriteFile(filepath.Join(configDir, ".credentials.json"), []byte(creds), 0644); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	fakeScript := filepath.Join(fakeBin, "security")
	if err := os.WriteFile(fakeScript, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", fakeBin+":"+origPath)
	defer os.Setenv("PATH", origPath)

	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	ResetDiscoveredService()

	tok, err := TokenForAccount(configDir, "", "")
	if err != nil {
		t.Fatalf("TokenForAccount: %v", err)
	}
	if tok != "file-token-456" {
		t.Fatalf("expected file-token-456, got %s", tok)
	}
}

func TestTokenForAccountEnvOverridesAll(t *testing.T) {
	os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-token")
	defer os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")

	tok, err := TokenForAccount("/some/config", "", "")
	if err != nil {
		t.Fatalf("TokenForAccount: %v", err)
	}
	if tok != "env-token" {
		t.Fatalf("expected env-token, got %s", tok)
	}
}
