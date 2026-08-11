package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The prober only stops re-probing a dead PR URL if this classifier recognises
// gh's wording, so the real messages are pinned here. The first case is verbatim
// from the log that prompted the fix.
func TestIsPRNotFound(t *testing.T) {
	notFound := []string{
		"GraphQL: Could not resolve to a PullRequest with the number of 33. (repository.pullRequest)",
		"no pull requests found for branch \"feat/x\"",
	}
	for _, out := range notFound {
		if !isPRNotFound(out) {
			t.Errorf("isPRNotFound(%q) = false, want true", out)
		}
	}

	// Everything else is about the probe, not the PR — treating any of these as a
	// verdict would retire live PRs the moment the network or the token blinks.
	transient := []string{
		"gh: To use GitHub CLI in a GitHub Actions workflow, set the GH_TOKEN environment variable",
		"dial tcp: lookup api.github.com: no such host",
		"API rate limit exceeded for user ID 1234",
		"GraphQL: Could not resolve to a Repository with the name 'acme/private'. (repository)",
	}
	for _, out := range transient {
		if isPRNotFound(out) {
			t.Errorf("isPRNotFound(%q) = true, want false", out)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	vars := []string{
		"GIT_SSH_COMMAND", "GIT_SSH",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
		"GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES",
	}

	for _, k := range vars {
		os.Setenv(k, "test-value")
	}

	ScrubEnv()

	for _, k := range vars {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s should be unset, got %q", k, v)
		}
	}

	if v := os.Getenv("GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT should be 0, got %q", v)
	}
}

func addWorktree(t *testing.T, repo, path, branch string) {
	t.Helper()
	mustRun(t, repo, "worktree", "add", "-b", branch, path, "main")
}

// worktreeListed asks the owning checkout whether it still has a worktree on
// branch. Matched by ref rather than path because macOS hands out /var temp dirs
// that git reports back as /private/var.
func worktreeListed(t *testing.T, repo, branch string) bool {
	t.Helper()
	out, err := Run(context.Background(), repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("worktree list: %v (%s)", err, out)
	}
	return strings.Contains(out, "branch refs/heads/"+branch+"\n") ||
		strings.HasSuffix(out, "branch refs/heads/"+branch)
}

// The case this exists for: a task that names no repo is told to write its
// deliverables straight into its working directory, and nothing else has a copy.
func TestRemoveAgentWorkdir_KeepsLooseFileWhenNoRepoIsInvolved(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, workdir, "burnrate-analytics.html", "<h1>the deliverable</h1>\n")

	kept, err := RemoveAgentWorkdir(context.Background(), workdir, 7)
	if kept != workdir {
		t.Fatalf("kept = %q, want %q", kept, workdir)
	}
	if !errors.Is(err, ErrUnbackedWork) {
		t.Fatalf("err = %v, want ErrUnbackedWork", err)
	}
	if _, err := os.Stat(workdir); err != nil {
		t.Fatalf("workdir was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "burnrate-analytics.html")); err != nil {
		t.Fatalf("deliverable was destroyed: %v", err)
	}
}

func TestRemoveAgentWorkdir_RemovesWhenOnlyACheckpointedWorktreeRemains(t *testing.T) {
	repo := initRepo(t)
	workdir := t.TempDir()
	addWorktree(t, repo, filepath.Join(workdir, "repo"), "agent/only")

	kept, err := RemoveAgentWorkdir(context.Background(), workdir, 7)
	if kept != "" || err != nil {
		t.Fatalf("kept = %q, err = %v; want removal", kept, err)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("workdir should be gone, stat err = %v", err)
	}
	if worktreeListed(t, repo, "agent/only") {
		t.Fatal("worktree is still registered in the checkout that owns it")
	}
}

func TestRemoveAgentWorkdir_DetachesWorktreeButKeepsALooseFileBesideIt(t *testing.T) {
	repo := initRepo(t)
	workdir := t.TempDir()
	addWorktree(t, repo, filepath.Join(workdir, "repo"), "agent/beside")
	writeFile(t, workdir, "notes.md", "findings that live nowhere else\n")

	kept, err := RemoveAgentWorkdir(context.Background(), workdir, 7)
	if kept != workdir {
		t.Fatalf("kept = %q, want %q", kept, workdir)
	}
	if !errors.Is(err, ErrUnbackedWork) {
		t.Fatalf("err = %v, want ErrUnbackedWork", err)
	}
	if worktreeListed(t, repo, "agent/beside") {
		t.Fatal("the worktree was pushed, so it should have been detached anyway")
	}
	if _, err := os.Stat(filepath.Join(workdir, "notes.md")); err != nil {
		t.Fatalf("loose file was destroyed: %v", err)
	}
}

func TestRemoveAgentWorkdir_RemovesEmptyDirectories(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "build", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	kept, err := RemoveAgentWorkdir(context.Background(), workdir, 7)
	if kept != "" || err != nil {
		t.Fatalf("kept = %q, err = %v; want removal", kept, err)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("workdir should be gone, stat err = %v", err)
	}
}

func TestRemoveAgentWorkdir_RemovesWhenOnlyDSStoreRemains(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, workdir, ".DS_Store", "finder junk")

	kept, err := RemoveAgentWorkdir(context.Background(), workdir, 7)
	if kept != "" || err != nil {
		t.Fatalf("kept = %q, err = %v; want removal", kept, err)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("workdir should be gone, stat err = %v", err)
	}
}
