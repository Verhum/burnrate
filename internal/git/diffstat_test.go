package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseNumstat(t *testing.T) {
	out := "10\t2\tfoo.go\n0\t5\tbar.go\n-\t-\timage.png\n"
	st := parseNumstat(out)
	if st.FilesChanged != 3 {
		t.Fatalf("expected 3 files, got %d", st.FilesChanged)
	}
	if st.Added != 10 || st.Removed != 7 {
		t.Fatalf("expected +10/-7, got +%d/-%d", st.Added, st.Removed)
	}
	if st.Changed() != 17 {
		t.Fatalf("expected 17 changed, got %d", st.Changed())
	}
}

func TestParseNumstat_Empty(t *testing.T) {
	st := parseNumstat("")
	if st != (DiffStat{}) {
		t.Fatalf("expected zero stat, got %+v", st)
	}
}

// initRepo builds a throwaway repo with one commit on `main`, isolated from the
// developer's git config.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := Run(ctx, dir, "git", args...); err != nil {
			t.Skipf("git %v unavailable: %v (%s)", args, err, out)
		}
	}
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	mustRun(t, dir, "add", "-A")
	mustRun(t, dir, "commit", "-m", "base")
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := Run(context.Background(), dir, "git", args...); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func TestBranchDiffStat_CountsOnlyTheBranchesOwnCommits(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	mustRun(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "b.txt", "x\ny\n")
	writeFile(t, dir, "a.txt", "one\nTWO\nthree\n")
	mustRun(t, dir, "add", "-A")
	mustRun(t, dir, "commit", "-m", "work")

	// A commit that lands on main after the branch point must not be counted as
	// the branch's output, which is what the three-dot diff guarantees.
	mustRun(t, dir, "checkout", "main")
	writeFile(t, dir, "c.txt", "unrelated\nlines\nhere\n")
	mustRun(t, dir, "add", "-A")
	mustRun(t, dir, "commit", "-m", "other work on main")
	mustRun(t, dir, "checkout", "feature")

	st, err := BranchDiffStat(ctx, dir, "main")
	if err != nil {
		t.Fatalf("diff stat: %v", err)
	}
	// b.txt: +2. a.txt: one line rewritten, so +1/-1.
	if st.Added != 3 || st.Removed != 1 {
		t.Fatalf("expected +3/-1, got +%d/-%d (%d files)", st.Added, st.Removed, st.FilesChanged)
	}
}

func TestBranchDiffStat_NoChangesIsZeroNotError(t *testing.T) {
	dir := initRepo(t)
	mustRun(t, dir, "checkout", "-b", "empty")

	st, err := BranchDiffStat(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("diff stat: %v", err)
	}
	if st.Changed() != 0 {
		t.Fatalf("expected no churn, got %+v", st)
	}
}

func TestBranchDiffStat_UncommittedWorkIsNotCounted(t *testing.T) {
	dir := initRepo(t)
	mustRun(t, dir, "checkout", "-b", "dirty")
	writeFile(t, dir, "d.txt", "not committed\n")

	st, err := BranchDiffStat(context.Background(), dir, "main")
	if err != nil {
		t.Fatalf("diff stat: %v", err)
	}
	if st.Changed() != 0 {
		t.Fatalf("uncommitted work is not delivered work, got %+v", st)
	}
}

func TestBranchDiffStat_UnknownBaseErrors(t *testing.T) {
	dir := initRepo(t)
	if _, err := BranchDiffStat(context.Background(), dir, "no-such-branch"); err == nil {
		t.Fatal("expected an error for a base ref that does not exist")
	}
}

func TestDiffBaseRef_PrefersRemoteTracking(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()
	// Fake a remote-tracking ref so no network is needed.
	mustRun(t, dir, "update-ref", "refs/remotes/origin/main", "main")

	ref, err := diffBaseRef(ctx, dir, "main")
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}
	if ref != "origin/main" {
		t.Fatalf("expected origin/main, got %q", ref)
	}
}

func TestDiffBaseRef_QualifiedBasePassesThrough(t *testing.T) {
	ref, err := diffBaseRef(context.Background(), t.TempDir(), "upstream/trunk")
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}
	if ref != "upstream/trunk" {
		t.Fatalf("expected passthrough, got %q", ref)
	}
}
