package prstatus

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/git"
	brlog "github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

func testProber(t *testing.T) (*Prober, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, brlog.New("", false)), st
}

func seedPR(t *testing.T, st *store.Store, repo, branch, url string) store.TaskPR {
	t.Helper()
	task, err := st.CreateTask("t", "p", "", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := st.UpsertTaskPR(task.ID, 0, repo, branch, url, "/tmp/wt"); err != nil {
		t.Fatalf("upsert pr: %v", err)
	}
	prs, err := st.ListTaskPRs(task.ID)
	if err != nil || len(prs) != 1 {
		t.Fatalf("list prs: %v (%d)", err, len(prs))
	}
	return prs[0]
}

func TestRefreshTaskRecordsState(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	if pr.PRState != "" {
		t.Fatalf("a fresh PR should be unprobed, got %q", pr.PRState)
	}

	var changed int
	p.OnChange = func() { changed++ }
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{State: "OPEN", IsDraft: true}, nil
	})

	got, err := p.RefreshTask(context.Background(), pr.TaskID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(got) != 1 || got[0].PRState != "OPEN" || !got[0].PRIsDraft {
		t.Fatalf("state not recorded: %+v", got)
	}
	if got[0].PRCheckedAt == "" {
		t.Fatal("checked_at not stamped")
	}
	if changed != 1 {
		t.Fatalf("OnChange fired %d times, want 1", changed)
	}
}

// A merged PR can never move again, so re-probing it is a wasted gh call on
// every sweep, forever.
func TestTerminalStatesAreNotReprobed(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	calls := 0
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		calls++
		return git.PRStatus{State: "MERGED"}, nil
	})
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if calls != 1 {
		t.Fatalf("probed %d times, want 1", calls)
	}
}

// A URL GitHub says is not a PR must be marked, not cleared. Clearing it leaves
// the row looking never-probed, and the sweep then pays for the same 404
// forever — which is how one stale repo rename turned into ~50 gh calls at every
// daemon startup.
func TestNotFoundIsMarkedGoneAndNotSweptAgain(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	calls := 0
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		calls++
		return git.PRStatus{}, fmt.Errorf("gh pr view: %w (could not resolve to a PullRequest)", git.ErrPRNotFound)
	})

	got, err := p.RefreshTask(context.Background(), pr.TaskID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got[0].PRState != domain.PRStateGone {
		t.Fatalf("state = %q, want %q", got[0].PRState, domain.PRStateGone)
	}
	if got[0].PRCheckedAt == "" {
		t.Fatal("checked_at should still be stamped")
	}

	// However many ticks pass, and whatever MinAge is, a gone PR is never a
	// sweep candidate again.
	p.MinAge = 0
	p.sweep(context.Background())
	p.sweep(context.Background())
	if calls != 1 {
		t.Fatalf("probed %d times, want 1 — a gone PR must not be re-swept", calls)
	}

	// An explicit refresh still may: it is the only way back if the URL was
	// unreadable for a reason that has since been fixed.
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{State: "OPEN"}, nil
	})
	got, err = p.RefreshTask(context.Background(), pr.TaskID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got[0].PRState != "OPEN" {
		t.Fatalf("an explicit refresh should re-probe a gone PR, state = %q", got[0].PRState)
	}
}

// A transient failure says nothing about the PR. Recording it as one would flip
// a live PR's chip to unknown every time the network hiccups.
func TestTransientFailureKeepsLastKnownState(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{State: "OPEN", IsDraft: true}, nil
	})
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{}, errors.New("dial tcp: lookup api.github.com: no such host")
	})
	got, err := p.RefreshTask(context.Background(), pr.TaskID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got[0].PRState != "OPEN" || !got[0].PRIsDraft {
		t.Fatalf("last-known state lost to a transient failure: %+v", got[0])
	}
	if got[0].PRCheckedAt == "" {
		t.Fatal("checked_at should still be stamped, or MinAge cannot pace the retry")
	}

	// And the PR stays under watch — a hiccup must not retire it.
	calls := 0
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		calls++
		return git.PRStatus{State: "MERGED"}, nil
	})
	p.MinAge = 0
	p.sweep(context.Background())
	if calls != 1 {
		t.Fatalf("probed %d times, want 1 — a transient failure must not retire the PR", calls)
	}
}

// Repeated failures have to get cheaper on their own. This is the safety net for
// the failures the daemon cannot classify at all — a repo gh will not resolve, a
// token that is never fixed — and for the day gh rewords its 404 and
// git.ErrPRNotFound stops matching.
func TestRepeatedFailuresBackOff(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	calls := 0
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		calls++
		return git.PRStatus{}, errors.New("could not resolve to a Repository")
	})
	p.MinAge = time.Minute

	// One failure, five minutes ago: the wait is two minutes, so a sweep now
	// probes — a single hiccup must not slow anything down noticeably.
	fiveAgo := time.Now().Add(-5 * time.Minute)
	if err := st.RecordTaskPRProbeFailure(pr.ID, fiveAgo); err != nil {
		t.Fatalf("seed failure: %v", err)
	}
	p.sweep(context.Background())
	if calls != 1 {
		t.Fatalf("probed %d times, want 1 — one prior failure should not suppress a sweep", calls)
	}

	// Five failures, still five minutes ago: the wait is 32 minutes, so the same
	// sweep now leaves it alone. That is the whole point — a URL that keeps
	// failing stops being a per-tick cost.
	for i := 0; i < 5; i++ {
		if err := st.RecordTaskPRProbeFailure(pr.ID, fiveAgo); err != nil {
			t.Fatalf("seed failure: %v", err)
		}
	}
	calls = 0
	p.sweep(context.Background())
	p.sweep(context.Background())
	if calls != 0 {
		t.Fatalf("probed %d times, want 0 — five failures should back off past five minutes", calls)
	}

	// A success wipes the slate, or a PR that recovers stays slow forever.
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{State: "OPEN"}, nil
	})
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	prs, _ := st.ListTaskPRs(pr.TaskID)
	if prs[0].PRProbeFailures != 0 {
		t.Fatalf("failure count survived a success: %d", prs[0].PRProbeFailures)
	}
}

func TestProbeBackoff(t *testing.T) {
	min := 4 * time.Minute
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 4 * time.Minute},
		{1, 8 * time.Minute},
		{3, 32 * time.Minute},
		{12, maxProbeBackoff},
		{1000, maxProbeBackoff}, // must not overflow into a negative wait
	}
	for _, c := range cases {
		if got := probeBackoff(min, c.failures); got != c.want {
			t.Errorf("probeBackoff(%v, %d) = %v, want %v", min, c.failures, got, c.want)
		}
	}
}

func TestSweepThrottlesByMinAge(t *testing.T) {
	p, st := testProber(t)
	seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	calls := 0
	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		calls++
		return git.PRStatus{State: "OPEN"}, nil
	})
	p.MinAge = time.Hour

	p.sweep(context.Background())
	p.sweep(context.Background())
	if calls != 1 {
		t.Fatalf("probed %d times, want 1 (second sweep is inside MinAge)", calls)
	}

	// An explicit refresh ignores MinAge: a stale answer is the complaint.
	if _, err := p.RefreshTask(context.Background(), 1); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if calls != 2 {
		t.Fatalf("probed %d times, want 2", calls)
	}
}

// A new PR URL on the same branch describes a different PR, so the cached state
// no longer applies to it.
func TestUpsertWithNewURLDropsCachedState(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "https://github.com/acme/api/pull/3")

	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		return git.PRStatus{State: "CLOSED"}, nil
	})
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if err := st.UpsertTaskPR(pr.TaskID, 0, "acme/api", "b1", "https://github.com/acme/api/pull/4", ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	prs, _ := st.ListTaskPRs(pr.TaskID)
	if prs[0].PRState != "" || prs[0].PRCheckedAt != "" {
		t.Fatalf("cached state survived a URL change: %+v", prs[0])
	}

	// ...and a re-report of the *same* URL keeps it.
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := st.UpsertTaskPR(pr.TaskID, 0, "acme/api", "b1", "https://github.com/acme/api/pull/4", ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	prs, _ = st.ListTaskPRs(pr.TaskID)
	if prs[0].PRState != "CLOSED" {
		t.Fatalf("cached state dropped on an unchanged URL: %+v", prs[0])
	}
}

func TestPRsWithoutURLAreNotProbed(t *testing.T) {
	p, st := testProber(t)
	pr := seedPR(t, st, "acme/api", "b1", "")

	p.SetProbe(func(context.Context, string) (git.PRStatus, error) {
		t.Fatal("probed a PR with no url")
		return git.PRStatus{}, nil
	})
	if _, err := p.RefreshTask(context.Background(), pr.TaskID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}
