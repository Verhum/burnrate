// Pins the behaviour of publish-snapshot.sh.
//
// The snapshot is the artefact that gets made public, so the properties that
// matter are negative ones: no second commit, no tag, no object carrying the
// old credential. A regression here is not a broken build, it is a
// republication.
package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// leakyRepo builds a repo shaped like Verhum/burnrate after the rewrite: HEAD
// is clean, but a tag still reaches the commit that introduced the credential,
// so the value is very much still in the object store.
func leakyRepo(t *testing.T) (dir, secret string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "src")
	git(t, t.TempDir(), "init", "--quiet", "--initial-branch=main", dir)

	secret = randomUUID(t)
	write(t, dir, "upload-release.sh", "#!/bin/sh\nSECRET=\""+secret+"\"\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "--no-verify", "-m", "leak")
	git(t, dir, "tag", "v0.1.9")

	git(t, dir, "rm", "--quiet", "upload-release.sh")
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	// The embed placeholder the script insists on; see CLAUDE.md.
	write(t, dir, "web/out/.gitkeep", "")
	write(t, dir, ".gitleaks.toml", "[extend]\nuseDefault = true\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "--no-verify", "-m", "scrub the credential")
	return dir, secret
}

func publishSnapshot(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "publish-snapshot.sh"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestPublishSnapshotDropsHistoryAndTheLeakWithIt(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	src, secret := leakyRepo(t)
	snap := filepath.Join(t.TempDir(), "snapshot")

	out, err := publishSnapshot(t, src, "--out", snap)
	if err != nil {
		t.Fatalf("publish-snapshot.sh: %v\n%s", err, out)
	}

	if refs := strings.Fields(git(t, snap, "for-each-ref", "--format=%(refname)")); len(refs) != 1 ||
		refs[0] != "refs/heads/main" {
		t.Errorf("snapshot refs = %v, want only refs/heads/main", refs)
	}
	if n := strings.TrimSpace(git(t, snap, "rev-list", "--count", "--all")); n != "1" {
		t.Errorf("snapshot has %s commits, want 1", n)
	}

	// The point of the whole exercise: the tag that kept the credential alive in
	// the source repo has no counterpart here, and neither does its object.
	if strings.Contains(git(t, snap, "log", "--all", "-p"), secret) {
		t.Error("the credential survived into the snapshot")
	}
	if strings.Contains(git(t, src, "log", "--all", "-p"), secret) == false {
		t.Fatal("fixture is wrong: the source repo should still carry the credential")
	}

	// The published tree is the source tree, exactly — a snapshot that quietly
	// drops or adds a file is worse than no snapshot.
	wantTree := strings.TrimSpace(git(t, src, "rev-parse", "HEAD^{tree}"))
	gotTree := strings.TrimSpace(git(t, snap, "rev-parse", "HEAD^{tree}"))
	if wantTree != gotTree {
		t.Errorf("snapshot tree %s != source tree %s\n%s",
			gotTree, wantTree, git(t, snap, "ls-files"))
	}

	if !strings.Contains(out, "will not create a remote for you") {
		t.Errorf("the script did not stop short of publishing:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(snap, ".git", "refs", "remotes")); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(snap, ".git", "refs", "remotes")); len(entries) > 0 {
			t.Error("the snapshot has a remote configured")
		}
	}
}

// Uncommitted work is the way a credential reaches a snapshot that scans clean
// at the commit it claims to be, so the export refuses to run against a dirty
// tree rather than silently publishing the commit instead.
func TestPublishSnapshotRefusesADirtyTree(t *testing.T) {
	src, _ := leakyRepo(t)
	write(t, src, "scratch.txt", "uncommitted\n")
	git(t, src, "add", "-A")

	out, err := publishSnapshot(t, src, "--out", filepath.Join(t.TempDir(), "snapshot"))
	if err == nil {
		t.Fatalf("publish-snapshot.sh accepted a dirty tree:\n%s", out)
	}
	if !strings.Contains(out, "working tree differs") {
		t.Errorf("refused, but not for the dirty tree:\n%s", out)
	}
}

func TestPublishSnapshotRefusesACredentialInTheSnapshotItself(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	src, _ := leakyRepo(t)
	write(t, src, "deploy.sh", "#!/bin/sh\nSECRET=\""+randomUUID(t)+"\"\n")
	git(t, src, "add", "-A")
	git(t, src, "commit", "--quiet", "--no-verify", "-m", "a second leak, this time on HEAD")

	out, err := publishSnapshot(t, src, "--out", filepath.Join(t.TempDir(), "snapshot"))
	if err == nil {
		t.Fatalf("publish-snapshot.sh signed off a snapshot containing a credential:\n%s", out)
	}
	if !strings.Contains(out, "the snapshot contains a credential") {
		t.Errorf("refused, but not because of the credential:\n%s", out)
	}
}

// The export dir is gitignored but //go:embed needs it, so losing the tracked
// placeholder ships a repo that does not compile. Failing here is cheap;
// finding out from a stranger's issue is not.
func TestPublishSnapshotRefusesWithoutTheEmbedPlaceholder(t *testing.T) {
	src, _ := leakyRepo(t)
	git(t, src, "rm", "--quiet", "web/out/.gitkeep")
	git(t, src, "commit", "--quiet", "--no-verify", "-m", "drop the placeholder")

	out, err := publishSnapshot(t, src, "--out", filepath.Join(t.TempDir(), "snapshot"))
	if err == nil {
		t.Fatalf("publish-snapshot.sh exported a tree that will not compile:\n%s", out)
	}
	if !strings.Contains(out, "web/out/.gitkeep missing") {
		t.Errorf("refused, but not for the missing placeholder:\n%s", out)
	}
}

func TestPublishSnapshotRefusesToOverwriteWithoutForce(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not installed; CI installs it and enforces there")
	}

	src, _ := leakyRepo(t)
	snap := filepath.Join(t.TempDir(), "snapshot")
	if out, err := publishSnapshot(t, src, "--out", snap); err != nil {
		t.Fatalf("first run: %v\n%s", err, out)
	}

	out, err := publishSnapshot(t, src, "--out", snap)
	if err == nil {
		t.Fatalf("the second run clobbered %s without --force:\n%s", snap, out)
	}
	if !strings.Contains(out, "pass --force") {
		t.Errorf("refused, but not for the existing directory:\n%s", out)
	}
	if out, err := publishSnapshot(t, src, "--out", snap, "--force"); err != nil {
		t.Fatalf("--force did not overwrite: %v\n%s", err, out)
	}
}
