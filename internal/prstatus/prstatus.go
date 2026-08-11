// Package prstatus keeps the cached state of every PR a task produced in step
// with GitHub. A PR outlives the run that opened it — it gets reviewed, merged
// or closed hours later — so nothing in the run lifecycle can report the state
// the UI wants to color a chip by.
package prstatus

import (
	"context"
	"errors"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	"github.com/Verhum/burnrate/internal/git"
	"github.com/Verhum/burnrate/internal/log"
)

// Store is the slice of the store this package needs.
type Store interface {
	ListTaskPRs(taskID int64) ([]domain.TaskPR, error)
	AllTaskPRs() (map[int64][]domain.TaskPR, error)
	SetTaskPRState(id int64, state string, isDraft bool, checkedAt time.Time) error
	RecordTaskPRProbeFailure(id int64, checkedAt time.Time) error
}

// ProbeFunc resolves one PR's state. Injected so tests never shell out to gh.
type ProbeFunc func(ctx context.Context, prURL string) (git.PRStatus, error)

type Prober struct {
	st     Store
	logger *log.Logger
	probe  ProbeFunc

	// Interval paces the background sweep. MinAge suppresses re-probing a PR the
	// sweep already looked at, which is what keeps a short interval (needed so a
	// merge shows up promptly) from turning into one gh call per PR per tick.
	Interval time.Duration
	MinAge   time.Duration

	// OnChange fires once per sweep or refresh in which at least one PR's state
	// actually moved, so the server can push the new task list over SSE.
	OnChange func()
}

func New(st Store, logger *log.Logger) *Prober {
	return &Prober{
		st:       st,
		logger:   logger,
		probe:    func(ctx context.Context, url string) (git.PRStatus, error) { return git.ProbePR(ctx, url) },
		Interval: 5 * time.Minute,
		MinAge:   4 * time.Minute,
	}
}

// SetProbe replaces the gh call. Tests use it; the daemon does not.
func (p *Prober) SetProbe(f ProbeFunc) { p.probe = f }

// RefreshTask probes every PR of one task, ignoring MinAge — it backs an
// explicit user action, where a stale answer is the whole complaint.
func (p *Prober) RefreshTask(ctx context.Context, taskID int64) ([]domain.TaskPR, error) {
	prs, err := p.st.ListTaskPRs(taskID)
	if err != nil {
		return nil, err
	}
	if p.refresh(ctx, prs, 0, true) > 0 && p.OnChange != nil {
		p.OnChange()
	}
	return p.st.ListTaskPRs(taskID)
}

// Start sweeps every non-terminal PR on an interval until ctx is cancelled.
func (p *Prober) Start(ctx context.Context) {
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	p.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sweep(ctx)
		}
	}
}

func (p *Prober) sweep(ctx context.Context) {
	byTask, err := p.st.AllTaskPRs()
	if err != nil {
		p.logger.Warnf("pr status sweep: %v", err)
		return
	}
	var all []domain.TaskPR
	for _, prs := range byTask {
		all = append(all, prs...)
	}
	if p.refresh(ctx, all, p.MinAge, false) > 0 && p.OnChange != nil {
		p.OnChange()
	}
}

// refresh probes each candidate PR and returns how many changed state. Probing
// is sequential on purpose: gh is rate limited and a sweep is never latency
// sensitive.
func (p *Prober) refresh(ctx context.Context, prs []domain.TaskPR, minAge time.Duration, force bool) int {
	now := time.Now()
	changed := 0
	for _, pr := range prs {
		if !shouldProbe(pr, now, minAge, force) {
			continue
		}
		if ctx.Err() != nil {
			return changed
		}
		st, err := p.probe(ctx, pr.PRURL)
		if err != nil {
			if errors.Is(err, git.ErrPRNotFound) {
				// GitHub says there is no such PR, and that cannot un-happen on its
				// own. Record it as a state: writing "" here instead (as this used to)
				// leaves the row indistinguishable from a never-probed one, so every
				// dead URL gets re-probed on every sweep and they all pile into a
				// burst at startup.
				p.logger.Warnf("probe %s: no such PR on GitHub — marking gone, not probing again", pr.PRURL)
				if serr := p.st.SetTaskPRState(pr.ID, domain.PRStateGone, false, now); serr != nil {
					p.logger.Warnf("mark pr gone %d: %v", pr.ID, serr)
					continue
				}
				if pr.PRState != domain.PRStateGone {
					changed++
				}
				continue
			}
			// Anything else — no auth, no network, rate limit, a repo gh cannot
			// resolve — is about the probe, not about the PR. Keep what we last knew
			// (clearing it would flip a live PR's chip to unknown every time the wifi
			// drops) and count the failure, which backs the next attempt off.
			p.logger.Warnf("probe %s: %v", pr.PRURL, err)
			if serr := p.st.RecordTaskPRProbeFailure(pr.ID, now); serr != nil {
				p.logger.Warnf("record pr probe failure %d: %v", pr.ID, serr)
			}
			continue
		}
		if st.State == pr.PRState && st.IsDraft == pr.PRIsDraft {
			// Still record the check so MinAge can throttle the next sweep.
			if serr := p.st.SetTaskPRState(pr.ID, st.State, st.IsDraft, now); serr != nil {
				p.logger.Warnf("record pr state %d: %v", pr.ID, serr)
			}
			continue
		}
		if err := p.st.SetTaskPRState(pr.ID, st.State, st.IsDraft, now); err != nil {
			p.logger.Warnf("record pr state %d: %v", pr.ID, err)
			continue
		}
		changed++
	}
	return changed
}

// shouldProbe decides whether one PR is worth a gh call right now. The rule is
// open items only: a PR GitHub has already settled, or one it says does not
// exist, cannot move on its own, so probing it spends a subprocess to learn
// nothing. A never-probed row ("" state) gets exactly one probe to find out
// which of those it is.
//
// force marks an explicit user refresh. It overrides both MinAge and the gone
// marker — a stale answer is the whole complaint there, and a re-probe is the
// only way a PRStateGone row ever comes back (say, after `gh auth login`, or
// after a URL's repo really does return). It does not override Terminal: merged
// is merged.
func shouldProbe(pr domain.TaskPR, now time.Time, minAge time.Duration, force bool) bool {
	if pr.PRURL == "" || pr.Terminal() {
		return false
	}
	if pr.PRState == domain.PRStateGone && !force {
		return false
	}
	if force || minAge <= 0 || pr.PRCheckedAt == "" {
		return true
	}
	checked, err := time.Parse(time.RFC3339, pr.PRCheckedAt)
	if err != nil {
		return true
	}
	return now.Sub(checked) >= probeBackoff(minAge, pr.PRProbeFailures)
}

// maxProbeBackoff caps the wait below. A URL that keeps failing for a reason the
// daemon cannot classify — a private repo with no token, most likely — still gets
// looked at about once a day, which is cheap enough to be wrong about forever.
const maxProbeBackoff = 24 * time.Hour

// probeBackoff is how long a PR must sit untouched before the sweep tries it
// again: MinAge, doubled per consecutive failure. One flaky probe barely changes
// anything, while a URL that has failed ten times running costs a call a day
// instead of one every five minutes. It is the safety net under ErrPRNotFound —
// if gh ever rewords its 404 and the classifier stops recognising it, the cost of
// a dead URL still decays instead of running forever.
func probeBackoff(minAge time.Duration, failures int) time.Duration {
	backoff := minAge
	for i := 0; i < failures; i++ {
		if backoff >= maxProbeBackoff {
			break
		}
		backoff *= 2
	}
	return min(backoff, maxProbeBackoff)
}
