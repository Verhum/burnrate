package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func ScrubEnv() {
	for _, k := range []string{
		"GIT_SSH_COMMAND", "GIT_SSH",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
		"GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES",
	} {
		os.Unsetenv(k)
	}
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func DefaultBranch(ctx context.Context, repo string) (string, error) {
	out, err := Run(ctx, repo, "git", "rev-parse", "--abbrev-ref", "origin/HEAD")
	if err == nil {
		if parts := strings.SplitN(strings.TrimSpace(out), "/", 2); len(parts) == 2 {
			return parts[1], nil
		}
	}

	out, err = Run(ctx, repo, "git", "remote", "show", "origin")
	if err != nil {
		return "", fmt.Errorf("detect default branch: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "HEAD branch:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:")), nil
		}
	}
	return "", fmt.Errorf("could not detect default branch for %s", repo)
}

// OwningRepo returns the main checkout that owns the given worktree, so
// worktree teardown can be run from the repo that created it without having to
// be told which repo that was.
func OwningRepo(ctx context.Context, worktree string) (string, error) {
	out, err := Run(ctx, worktree, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve owning repo for %s: %w", worktree, err)
	}
	return filepath.Dir(strings.TrimSpace(out)), nil
}

func Fetch(ctx context.Context, repo string) error {
	_, err := Run(ctx, repo, "git", "fetch", "origin")
	return err
}

func AddWorktree(ctx context.Context, repo, path, branch, base string) error {
	_, err := Run(ctx, repo, "git", "worktree", "add", path, "-b", branch, "origin/"+base)
	if err == nil {
		return nil
	}

	// Stale worktree or branch — clean up and retry once.
	if _, statErr := os.Stat(path); statErr == nil {
		Run(ctx, repo, "git", "worktree", "remove", "--force", path)
	}
	Run(ctx, repo, "git", "branch", "-D", branch)
	Run(ctx, repo, "git", "worktree", "prune")

	_, err = Run(ctx, repo, "git", "worktree", "add", path, "-b", branch, "origin/"+base)
	if err != nil {
		return fmt.Errorf("add worktree (retry): %w", err)
	}
	return nil
}

func RemoveWorktree(ctx context.Context, repo, path string) error {
	_, err := Run(ctx, repo, "git", "worktree", "remove", "--force", path)
	if err != nil {
		os.RemoveAll(path)
	}
	Run(ctx, repo, "git", "worktree", "prune")
	return nil
}

// CurrentBranch returns the branch checked out in a worktree, or "" when HEAD is
// detached.
func CurrentBranch(ctx context.Context, worktree string) string {
	out, err := Run(ctx, worktree, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// agentWorkdirScanDepth bounds how far into an agent's working directory we look
// for worktrees. The worker prompt asks for ${WORKDIR}/<repo-name>, but agents
// choose their own layout and a worktree missed here gets deleted from disk while
// staying registered in the checkout that owns it.
const agentWorkdirScanDepth = 3

// ErrUnbackedWork reports that the agent workdir was kept because it still holds
// files that no branch stands behind.
var ErrUnbackedWork = errors.New("files not backed by any branch")

// RemoveAgentWorkdir tears down an agent-directed run's working directory. The
// agent creates its own git worktrees inside it, so each one is checkpointed —
// committed and pushed, exactly as RemoveWorktree's managed-worktree counterpart
// does — then detached from the checkout that owns it, before the directory
// itself is deleted.
//
// If any worktree still holds work that cannot be committed and pushed, nothing
// is removed and its path is returned: unpushed work is worth more than
// reclaimed disk. The caller decides how loudly to say so.
//
// The directory is deleted only when detaching the worktrees left nothing behind.
// Git is the only thing that persists an agent's work, so a file with no branch
// under it has no backup anywhere and deleting it destroys the only copy — and a
// task told to name no repo is told to write its deliverables straight into the
// workdir. The next iteration of the task lands in this same path, so keeping it
// is also what lets that iteration build on what is already there. When anything
// remains the workdir is returned with ErrUnbackedWork and nothing is removed.
func RemoveAgentWorkdir(ctx context.Context, workdir string, runID int64) (kept string, err error) {
	if workdir == "" {
		return "", nil
	}
	if _, statErr := os.Stat(workdir); statErr != nil {
		return "", nil
	}

	type ownedWorktree struct{ path, owner string }
	var worktrees []ownedWorktree

	for _, path := range findAgentWorktrees(workdir, agentWorkdirScanDepth) {
		owner, err := OwningRepo(ctx, path)
		if err != nil {
			// Without the owning checkout the worktree cannot be detached, and
			// deleting it anyway would strand its registration there forever.
			return path, err
		}
		if HasUncommittedChanges(ctx, path) {
			// A checkpoint commit on a detached HEAD has nowhere to be pushed,
			// so removing the worktree afterwards would lose it.
			if CurrentBranch(ctx, path) == "" {
				return path, fmt.Errorf("detached HEAD with uncommitted changes")
			}
			if err := Checkpoint(ctx, path, runID); err != nil {
				return path, err
			}
			if HasUncommittedChanges(ctx, path) {
				return path, fmt.Errorf("uncommitted changes survived the checkpoint")
			}
		}
		worktrees = append(worktrees, ownedWorktree{path: path, owner: owner})
	}

	for _, wt := range worktrees {
		RemoveWorktree(ctx, wt.owner, wt.path)
	}
	if hasFiles(workdir) {
		return workdir, ErrUnbackedWork
	}
	return "", os.RemoveAll(workdir)
}

// hasFiles reports whether any non-directory entry survives under dir at any
// depth. Directories left empty by the worktree teardown carry nothing, and
// .DS_Store is Finder's, not the agent's.
func hasFiles(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".DS_Store" {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	return found
}

// findAgentWorktrees returns the worktrees under dir, identified by a .git *file*
// — a full clone has a .git directory and needs no detaching.
func findAgentWorktrees(dir string, depth int) []string {
	if depth <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if fi, err := os.Lstat(filepath.Join(child, ".git")); err == nil && !fi.IsDir() {
			found = append(found, child)
			continue
		}
		found = append(found, findAgentWorktrees(child, depth-1)...)
	}
	return found
}

func HasUnpushedCommits(ctx context.Context, worktree, defaultBranch string) (bool, error) {
	out, err := Run(ctx, worktree, "git", "log", "origin/"+defaultBranch+"..HEAD", "--oneline")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

var originSlugRe = regexp.MustCompile(`[:/]([^/:]+/[^/]+?)(?:\.git)?$`)

// OriginSlug returns the `owner/name` of a checkout's origin remote, or "" if
// it cannot be determined. Used to label which repo a PR belongs to.
func OriginSlug(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	m := originSlugRe.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return ""
	}
	return m[1]
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(title string) string {
	s := strings.ToLower(title)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func BranchExistsAtOrigin(ctx context.Context, repo, branch string) bool {
	out, err := Run(ctx, repo, "git", "ls-remote", "--heads", "origin", branch)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

func AddWorktreeFromBranch(ctx context.Context, repo, path, branch string) error {
	if _, statErr := os.Stat(path); statErr == nil {
		Run(ctx, repo, "git", "worktree", "remove", "--force", path)
	}
	Run(ctx, repo, "git", "worktree", "prune")
	Run(ctx, repo, "git", "branch", "-D", branch)

	_, err := Run(ctx, repo, "git", "worktree", "add", path, branch)
	if err != nil {
		return fmt.Errorf("add worktree from branch %s: %w", branch, err)
	}
	return nil
}

func PushBranch(ctx context.Context, worktree, branch string) error {
	_, err := Run(ctx, worktree, "git", "push", "-u", "origin", branch)
	return err
}

func Checkpoint(ctx context.Context, worktree string, runID int64) error {
	status, err := Run(ctx, worktree, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		if _, err := Run(ctx, worktree, "git", "add", "-A"); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		msg := fmt.Sprintf("wip: burnrate checkpoint (run %d)", runID)
		if _, err := Run(ctx, worktree, "git", "commit", "-m", msg); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}
	Run(ctx, worktree, "git", "push", "-u", "origin", "HEAD")
	return nil
}

func HasUncommittedChanges(ctx context.Context, worktree string) bool {
	status, err := Run(ctx, worktree, "git", "status", "--porcelain")
	if err != nil {
		return true
	}
	return strings.TrimSpace(status) != ""
}

// PRStatus is what gh reports about an existing PR. State uses gh's own
// vocabulary (OPEN / MERGED / CLOSED); IsDraft is orthogonal to it.
type PRStatus struct {
	State   string `json:"state"`
	IsDraft bool   `json:"isDraft"`
}

// ErrPRNotFound reports that GitHub answered and the PR simply is not there: it
// was deleted, or — the case that actually happens — the URL names a repo whose
// old name has since been re-used by a different repo, so the number resolves
// nowhere. That verdict is permanent, which is what separates it from every
// other probe failure (no auth, no network, rate limit); those say nothing about
// the PR and must not be recorded as if they did.
//
// Deliberately NOT returned when the *repository* fails to resolve. GitHub
// reports a private repo you cannot see with the same "Could not resolve to a
// Repository" as one that does not exist, so a missing token would otherwise
// look like proof that every PR in it is gone.
var ErrPRNotFound = errors.New("pull request not found")

// prNotFoundPatterns are gh's ways of saying the PR does not exist. Matched
// against gh's output because it reports this as a GraphQL error body, not as an
// exit code we could switch on. Load-bearing: a wording change here downgrades a
// dead URL to a transient failure, which is what makes the prober keep paying
// for it — hence TestIsPRNotFound.
var prNotFoundPatterns = []string{
	"could not resolve to a pullrequest",
	"no pull requests found",
}

func isPRNotFound(ghOutput string) bool {
	lower := strings.ToLower(ghOutput)
	for _, pat := range prNotFoundPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// ProbePR asks gh for a PR's current state. It is addressed by URL, not by
// branch, so it needs no local checkout of the repo — which matters because the
// worktree a PR was opened from is deleted once the run finishes.
//
// A failure means one of two very different things; see ErrPRNotFound.
func ProbePR(ctx context.Context, prURL string) (PRStatus, error) {
	if !strings.HasPrefix(prURL, "http") {
		return PRStatus{}, fmt.Errorf("not a PR url: %q", prURL)
	}
	out, err := Run(ctx, "", "gh", "pr", "view", prURL, "--json", "state,isDraft")
	if err != nil {
		if isPRNotFound(out) {
			return PRStatus{}, fmt.Errorf("gh pr view %s: %w (%s)", prURL, ErrPRNotFound, out)
		}
		return PRStatus{}, fmt.Errorf("gh pr view %s: %w (%s)", prURL, err, out)
	}
	var st PRStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return PRStatus{}, fmt.Errorf("gh pr view %s: unexpected output %q", prURL, out)
	}
	if st.State == "" {
		return PRStatus{}, fmt.Errorf("gh pr view %s: no state in %q", prURL, out)
	}
	return st, nil
}

func Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
