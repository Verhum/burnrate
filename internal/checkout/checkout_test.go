package checkout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/git"
)

// Real git, no mocks: the whole value of this package is what git does to a
// working tree, and a fake would only test the calls we wrote.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.Run(context.Background(), dir, "git", args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v (%s)", args, dir, err, out)
	}
	return out
}

// scratchRepo builds a bare "origin" whose path yields the slug owner/name, one
// branch beyond the default, and a clone of it under baseCodeDir.
func scratchRepo(t *testing.T, baseCodeDir, owner, name, branch string) (clone, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(origin), 0755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "init", "--bare", "-b", "main", origin)

	seed := filepath.Join(root, "seed")
	run(t, root, "clone", origin, seed)
	run(t, seed, "config", "user.email", "t@example.com")
	run(t, seed, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, seed, "add", "-A")
	run(t, seed, "commit", "-m", "init")
	run(t, seed, "push", "origin", "main")
	run(t, seed, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(seed, "feature"), []byte("y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, seed, "add", "-A")
	run(t, seed, "commit", "-m", "feature")
	run(t, seed, "push", "origin", branch)

	clone = filepath.Join(baseCodeDir, name)
	run(t, root, "clone", origin, clone)
	return clone, origin
}

func pr(id int64, repo, branch string) domain.TaskPR {
	return domain.TaskPR{ID: id, Repo: repo, Branch: branch, PRURL: "https://github.com/" + repo + "/pull/1"}
}

func TestCheckoutSwitchesTheUsersClone(t *testing.T) {
	base := t.TempDir()
	clone, _ := scratchRepo(t, base, "acme", "api", "burnrate/9-x")

	results := Task(context.Background(), base, []domain.TaskPR{pr(1, "acme/api", "burnrate/9-x")})
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %+v", results)
	}
	if results[0].Status != StatusCheckedOut {
		t.Fatalf("status = %s (%s)", results[0].Status, results[0].Detail)
	}
	if results[0].Path != clone {
		t.Fatalf("path = %s, want %s", results[0].Path, clone)
	}
	if got := git.CurrentBranch(context.Background(), clone); got != "burnrate/9-x" {
		t.Fatalf("clone is on %q", got)
	}

	// Idempotent: running it again is not an error.
	again := Task(context.Background(), base, []domain.TaskPR{pr(1, "acme/api", "burnrate/9-x")})
	if again[0].Status != StatusAlready {
		t.Fatalf("second run status = %s (%s)", again[0].Status, again[0].Detail)
	}
}

// The clone is the user's, and it may hold work no one else has a copy of.
func TestCheckoutRefusesADirtyTree(t *testing.T) {
	base := t.TempDir()
	clone, _ := scratchRepo(t, base, "acme", "api", "burnrate/9-x")
	if err := os.WriteFile(filepath.Join(clone, "README"), []byte("edited\n"), 0644); err != nil {
		t.Fatal(err)
	}

	results := Task(context.Background(), base, []domain.TaskPR{pr(1, "acme/api", "burnrate/9-x")})
	if results[0].Status != StatusError {
		t.Fatalf("status = %s, want error", results[0].Status)
	}
	if got := git.CurrentBranch(context.Background(), clone); got != "main" {
		t.Fatalf("branch changed under a dirty tree: %q", got)
	}
}

// A clone holds one branch at a time, so the newest PR wins and the rest are
// reported rather than silently dropped.
func TestSeveralPRsInOneRepoKeepTheNewest(t *testing.T) {
	base := t.TempDir()
	clone, origin := scratchRepo(t, base, "acme", "api", "burnrate/9-first")
	run(t, clone, "fetch", "origin")
	run(t, clone, "checkout", "-b", "burnrate/9-second", "main")
	run(t, clone, "push", "origin", "burnrate/9-second")
	run(t, clone, "checkout", "main")
	_ = origin

	results := Task(context.Background(), base, []domain.TaskPR{
		pr(1, "acme/api", "burnrate/9-first"),
		pr(2, "acme/api", "burnrate/9-second"),
	})
	if len(results) != 2 {
		t.Fatalf("both PRs should be reported, got %+v", results)
	}
	if results[0].Branch != "burnrate/9-second" || results[0].Status != StatusCheckedOut {
		t.Fatalf("newest PR should win: %+v", results[0])
	}
	if results[1].Status != StatusSkipped {
		t.Fatalf("older PR should be skipped, got %+v", results[1])
	}
	if got := git.CurrentBranch(context.Background(), clone); got != "burnrate/9-second" {
		t.Fatalf("clone is on %q", got)
	}
}

// A directory name is a guess; the remote is the fact.
func TestLocalCloneIsVerifiedAgainstItsOrigin(t *testing.T) {
	base := t.TempDir()
	scratchRepo(t, base, "acme", "api", "burnrate/9-x")

	// A decoy at the conventional path for a different repo.
	decoy := filepath.Join(base, "web")
	run(t, base, "init", "-b", "main", decoy)

	if _, err := LocalClone(context.Background(), base, "acme/web"); err == nil {
		t.Fatal("a clone with the right name but no matching origin should not match")
	}
	_ = decoy
}

func TestLocalCloneFoundUnderAnUnrelatedDirectoryName(t *testing.T) {
	base := t.TempDir()
	clone, _ := scratchRepo(t, base, "acme", "api", "burnrate/9-x")
	renamed := filepath.Join(base, "api-but-renamed")
	if err := os.Rename(clone, renamed); err != nil {
		t.Fatal(err)
	}

	got, err := LocalClone(context.Background(), base, "acme/api")
	if err != nil {
		t.Fatalf("scan should have found it: %v", err)
	}
	if got != renamed {
		t.Fatalf("found %s, want %s", got, renamed)
	}
}

func TestMissingCloneIsReportedNotFatal(t *testing.T) {
	base := t.TempDir()
	scratchRepo(t, base, "acme", "api", "burnrate/9-x")

	results := Task(context.Background(), base, []domain.TaskPR{
		pr(1, "acme/api", "burnrate/9-x"),
		pr(2, "acme/nowhere", "burnrate/9-y"),
	})
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %+v", results)
	}
	if results[0].Status != StatusCheckedOut {
		t.Fatalf("the repo that does exist should still be checked out: %+v", results[0])
	}
	if results[1].Status != StatusError {
		t.Fatalf("want error for the missing clone, got %+v", results[1])
	}
}

func TestPRsWithoutARepoOrBranchAreIgnored(t *testing.T) {
	base := t.TempDir()
	results := Task(context.Background(), base, []domain.TaskPR{
		{ID: 1, PRURL: "https://github.com/acme/api/pull/1"},
		{ID: 2, Repo: "acme/api"},
	})
	if len(results) != 0 {
		t.Fatalf("want no results, got %+v", results)
	}
}
