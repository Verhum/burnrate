package scheduling

import "time"

const (
	// resetUtilDrop is how far session utilization must fall between two
	// readings to count as a new session. Readings jitter by a few percent;
	// a real reset drops it to near zero.
	resetUtilDrop = 40.0
	// resetAdvanceEpsilon is how far the reported reset time must move
	// forward to count as a new session, rather than the same session's
	// estimate being refined.
	resetAdvanceEpsilon = 2 * time.Minute
)

// ResetDetector spots the edge where one session ends and the next begins.
//
// It is the only stateful thing in this package, and it holds exactly the two
// previous readings needed to see an edge — session state itself is a pure
// function of the current reading (see WindowStateFor), so nothing else needs
// to remember anything.
type ResetDetector struct {
	lastUtil  float64
	lastReset time.Time
}

// Observe records a reading and reports whether a new session just started.
func (d *ResetDetector) Observe(snap Snapshot) bool {
	reset := false

	if !d.lastReset.IsZero() && !snap.SessionResetsAt.IsZero() {
		if snap.SessionResetsAt.Sub(d.lastReset) > resetAdvanceEpsilon {
			reset = true
		}
	}
	if d.lastUtil > 0 && snap.SessionUtil < d.lastUtil-resetUtilDrop {
		reset = true
	}

	d.lastUtil = snap.SessionUtil
	if !snap.SessionResetsAt.IsZero() {
		d.lastReset = snap.SessionResetsAt
	}

	return reset
}
