// Package scripts_test pins the behaviour of .gitleaks.toml.
//
// The config is what stands between this repo and a repeat of the
// landing/scripts/upload-release.sh leak, and an allowlist is one careless
// broadening away from matching everything. These cases run the real scanner
// over synthetic files so a widened allowlist fails here rather than silently.
package scripts_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture credentials are generated per run, never written as literals: a
// literal in this file would be a finding in every tree scan, and allowlisting
// this path to silence it would hollow out the test.
func randomToken(t *testing.T, n int) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	for i, c := range b {
		b[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(b)
}

// gitleaks scores candidates with Shannon entropy and drops generic-api-key
// matches below 3.5. A random UUID usually clears it but not always, so the
// UUID fixture is regenerated until it does — otherwise this test would fail a
// few runs in a hundred for a reason unrelated to the config.
func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	var freq [256]float64
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	var e float64
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := c / float64(len(s))
		e -= p * math.Log2(p)
	}
	return e
}

func randomUUID(t *testing.T) string {
	t.Helper()
	const hex = "0123456789abcdef"
	for attempt := 0; attempt < 50; attempt++ {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("rand: %v", err)
		}
		for i, c := range b {
			b[i] = hex[int(c)%16]
		}
		s := string(b)
		u := fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
		if shannon(u) >= 3.7 {
			return u
		}
	}
	t.Fatal("no sufficiently high-entropy UUID in 50 attempts")
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return root
}

// scan runs gitleaks over dir with the repo config and returns the set of
// "<file>:<line>" findings.
func scan(t *testing.T, dir string) map[string]string {
	t.Helper()
	report := filepath.Join(t.TempDir(), "report.json")
	cmd := exec.Command("gitleaks", "dir", dir,
		"--no-banner", "--redact",
		"--config", filepath.Join(repoRoot(t), ".gitleaks.toml"),
		"--report-format", "json", "--report-path", report)
	out, err := cmd.CombinedOutput()
	// Exit 1 means findings, which most of these cases expect.
	if err != nil && cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("gitleaks: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var findings []struct {
		RuleID    string `json:"RuleID"`
		File      string `json:"File"`
		StartLine int    `json:"StartLine"`
	}
	if err := json.Unmarshal(raw, &findings); err != nil {
		t.Fatalf("parse report %q: %v", raw, err)
	}

	got := make(map[string]string, len(findings))
	for _, f := range findings {
		rel, err := filepath.Rel(dir, f.File)
		if err != nil {
			rel = f.File
		}
		got[rel] = f.RuleID
	}
	return got
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestGitleaksConfig(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	dir := t.TempDir()
	uuid := randomUUID(t)

	// The exact shape that leaked: a bare UUID assigned to SECRET= in a shell
	// script. Nothing about it looks like a vendor token, so it is caught on
	// entropy alone.
	write(t, dir, "upload-release.sh", "#!/bin/sh\nSECRET=\""+uuid+"\"\n")

	// A longer opaque token in a config file, the other common shape.
	write(t, dir, "config.yaml", "api_key: \""+randomToken(t, 40)+"\"\n")

	// The correct form must not be a finding, or the fix for a real leak trips
	// the scanner and people start reaching for --no-verify.
	write(t, dir, "good.sh", "#!/bin/sh\nSECRET=\"${UPLOAD_SECRET:?set UPLOAD_SECRET}\"\n")

	// The one substantive allowlist entry.
	write(t, dir, "refresh.go", "const oauthClientID = \""+uuid+"\"\n")

	// Path allowlists: generated trees are excluded wholesale.
	write(t, dir, "node_modules/dep/index.js", "const SECRET = \""+uuid+"\";\n")
	write(t, dir, "web/.next/cache/blob", "SECRET=\""+uuid+"\"\n")

	got := scan(t, dir)

	want := map[string]bool{"upload-release.sh": true, "config.yaml": true}
	for f := range want {
		if _, ok := got[f]; !ok {
			t.Errorf("%s: expected a finding, got none — the config no longer catches a committed credential", f)
		}
	}
	for f, rule := range got {
		if !want[f] {
			t.Errorf("%s: unexpected finding (rule %s)", f, rule)
		}
	}
}

// The allowlist entry for oauthClientID is deliberately scoped to that one
// assignment rather than to internal/usage/, so a real secret added beside it
// is still caught.
func TestOAuthClientIDAllowlistIsNarrow(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	dir := t.TempDir()
	write(t, dir, "refresh.go", strings.Join([]string{
		"const oauthClientID = \"" + randomUUID(t) + "\"",
		"const oauthClientSecret = \"" + randomToken(t, 40) + "\"",
		"",
	}, "\n"))

	if _, ok := scan(t, dir)["refresh.go"]; !ok {
		t.Error("a secret on the line below oauthClientID went undetected; the allowlist has been widened past that one assignment")
	}
}

// git runs a command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// gitPush is git() for a command that is expected to be able to fail.
func gitPush(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// scrubbedClone builds the situation this repo actually got into: a secret was
// committed and tagged, the branch was then rewritten to drop it and
// force-pushed, and the tag was left pointing at the pre-rewrite commit. The
// returned dir is a clone in that state, with the tracked hooks installed.
func scrubbedClone(t *testing.T) (work, remote, secret string) {
	t.Helper()
	root := repoRoot(t)
	base := t.TempDir()
	remote = filepath.Join(base, "remote.git")
	work = filepath.Join(base, "work")

	git(t, base, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	git(t, base, "init", "--quiet", "--initial-branch=main", work)
	git(t, work, "remote", "add", "origin", remote)
	// The hooks and the scanner are used from the real checkout, not copied, so
	// this exercises the files that ship.
	git(t, work, "config", "core.hooksPath", filepath.Join(root, ".githooks"))

	write(t, work, "clean.txt", "nothing to see\n")
	git(t, work, "add", "-A")
	git(t, work, "commit", "--quiet", "--no-verify", "-m", "clean")
	if out, err := gitPush(t, work, "push", "--quiet", "-u", "origin", "main"); err != nil {
		t.Fatalf("push of clean history was refused: %v\n%s", err, out)
	}
	cleanTip := strings.TrimSpace(git(t, work, "rev-parse", "HEAD"))

	secret = randomUUID(t)
	write(t, work, "upload-release.sh", "#!/bin/sh\nSECRET=\""+secret+"\"\n")
	git(t, work, "add", "-A")
	git(t, work, "commit", "--quiet", "--no-verify", "-m", "leak")
	git(t, work, "tag", "v1.0.0")

	// The rewrite: main goes back to clean history and is force-pushed. The tag
	// still reaches the leak, exactly like the v0.1.x tags in ~/code/burnrate.
	git(t, work, "reset", "--hard", "--quiet", cleanTip)
	if out, err := gitPush(t, work, "push", "--quiet", "--force", "origin", "main"); err != nil {
		t.Fatalf("force-push of scrubbed history was refused: %v\n%s", err, out)
	}
	git(t, work, "fetch", "--quiet", "origin")
	return work, remote, secret
}

// The accident this guards against: `git push --tags` from a clone that predates
// a history rewrite re-publishes the pre-rewrite commits, putting the credential
// back on the remote as branch-reachable history. The remote will not refuse it.
func TestPrePushHookRefusesStaleTag(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	work, remote, _ := scrubbedClone(t)

	out, err := gitPush(t, work, "push", "origin", "--tags")
	if err == nil {
		t.Fatalf("push --tags succeeded; the stale tag re-published the secret:\n%s", out)
	}
	if !strings.Contains(out, "refusing to push commits that carry a credential") {
		t.Errorf("push failed but not via the pre-push hook:\n%s", out)
	}
	if tags := git(t, remote, "tag"); strings.TrimSpace(tags) != "" {
		t.Errorf("remote received tags %q despite the refusal", tags)
	}

	// A push that carries no credential must still go through, or the hook just
	// teaches people to reach for --no-verify.
	write(t, work, "more.txt", "still nothing\n")
	git(t, work, "add", "-A")
	git(t, work, "commit", "--quiet", "--no-verify", "-m", "clean follow-up")
	if out, err := gitPush(t, work, "push", "origin", "main"); err != nil {
		t.Fatalf("clean push was refused: %v\n%s", err, out)
	}
}

// purge-leaked-refs.sh has to find the stale ref, drop it, and leave the object
// unreachable — and it must not discard a stash without writing the diff out
// first, since a stash's parent is what keeps pre-rewrite history alive.
func TestPurgeLeakedRefsClearsStaleRefsAndKeepsStashDiffs(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	work, _, secret := scrubbedClone(t)

	// A stash taken against the pre-rewrite tip, holding work worth keeping.
	git(t, work, "checkout", "--quiet", "v1.0.0")
	write(t, work, "wip.txt", "unfinished but wanted\n")
	git(t, work, "add", "-A")
	git(t, work, "stash", "push", "--quiet", "-m", "wip")
	git(t, work, "checkout", "--quiet", "main")

	script := filepath.Join(repoRoot(t), "scripts", "purge-leaked-refs.sh")
	cmd := exec.Command(script, "--apply")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("purge-leaked-refs.sh --apply: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "DROP  refs/tags/v1.0.0") {
		t.Errorf("the stale tag was not dropped:\n%s", out)
	}

	// The whole point of the cleanup: the value is no longer in the object store.
	if strings.Contains(git(t, work, "log", "--all", "-p"), secret) {
		t.Error("the secret is still reachable from a ref after --apply")
	}
	if tags := strings.TrimSpace(git(t, work, "tag")); tags != "" {
		t.Errorf("tags remain after --apply: %q", tags)
	}

	// The stash is gone, but its diff was written out first.
	patch, err := os.ReadFile(filepath.Join(work, ".git", "stash-patches", "stash-0.patch"))
	if err != nil {
		t.Fatalf("stash patch not written: %v\n%s", err, out)
	}
	if !strings.Contains(string(patch), "unfinished but wanted") {
		t.Errorf("stash patch does not contain the stashed work:\n%s", patch)
	}
}
