// Package retention prunes aged capture recordings. Video files are the only
// thing it deletes: keyframes (loose image files under the capture directory)
// and transcripts (a DB column) are the cheap, durable record of a capture and
// are kept forever. The window comes from the `capture_retention_days` setting
// and is re-read on every sweep, so a PUT /api/config takes effect without a
// daemon restart.
package retention

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	brlog "github.com/Verhum/burnrate/internal/log"
)

const (
	// SettingKey is the settings-table key holding the retention window in days.
	SettingKey = "capture_retention_days"
	// DefaultRetentionDays applies when the setting is unset or unparseable.
	DefaultRetentionDays = 30
	// DefaultInterval is how often Start sweeps after its initial pass.
	DefaultInterval = 24 * time.Hour
)

// Store is the narrow slice of the persistence layer the pruner needs.
// *store.Store satisfies it.
type Store interface {
	ListCaptures(taskID int64) ([]domain.Capture, error)
	SetCaptureVideoPath(id int64, path string) error
	GetSetting(key string) (string, bool)
}

// Pruner deletes capture videos older than the configured retention window.
type Pruner struct {
	st      Store
	dataDir string
	logger  *brlog.Logger

	// Interval between sweeps once Start's initial sweep has run.
	Interval time.Duration
	// now is injectable so tests can drive the cutoff without sleeping.
	now func() time.Time
}

func New(st Store, dataDir string, logger *brlog.Logger) *Pruner {
	if logger == nil {
		logger = brlog.New("", false)
	}
	return &Pruner{
		st:       st,
		dataDir:  dataDir,
		logger:   logger,
		Interval: DefaultInterval,
		now:      time.Now,
	}
}

// Start sweeps once immediately, then every Interval, until ctx is cancelled.
// Mirrors prstatus.Prober.Start: launch it with `go p.Start(ctx)`.
func (p *Pruner) Start(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
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

func (p *Pruner) sweep(ctx context.Context) {
	if _, err := p.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		p.logger.Warnf("capture retention: sweep failed: %v", err)
	}
}

// SweepOnce runs a single prune pass and reports how many video files it
// deleted. A per-capture problem is logged and skipped; only a failure to read
// the capture list (or a cancelled context) is returned as an error.
func (p *Pruner) SweepOnce(ctx context.Context) (filesDeleted int, err error) {
	days := p.retentionDays()
	if days <= 0 {
		p.logger.Infof("capture retention: disabled (%s=%d), keeping every capture video", SettingKey, days)
		return 0, nil
	}

	captures, err := p.st.ListCaptures(0)
	if err != nil {
		return 0, fmt.Errorf("list captures: %w", err)
	}

	root := p.capturesRoot()
	cutoff := p.now().Add(-time.Duration(days) * 24 * time.Hour)

	var rowsCleared int
	var bytesFreed int64
	for _, c := range captures {
		select {
		case <-ctx.Done():
			return filesDeleted, ctx.Err()
		default:
		}

		if c.VideoPath == "" {
			continue
		}
		created, perr := time.Parse(time.RFC3339, c.CreatedAt)
		if perr != nil {
			// An unreadable timestamp is never treated as ancient.
			p.logger.Warnf("capture retention: capture %d has unparseable created_at %q, skipping: %v", c.ID, c.CreatedAt, perr)
			continue
		}
		if !created.Before(cutoff) {
			continue
		}

		abs, ok := withinRoot(root, c.VideoPath)
		if !ok {
			p.logger.Warnf("capture retention: capture %d video_path %q is outside %s, refusing to delete", c.ID, c.VideoPath, root)
			continue
		}

		fi, serr := os.Lstat(abs)
		switch {
		case serr == nil && fi.Mode().IsRegular():
			if rerr := os.Remove(abs); rerr != nil {
				p.logger.Warnf("capture retention: capture %d: remove %s: %v", c.ID, abs, rerr)
				continue
			}
			filesDeleted++
			bytesFreed += fi.Size()
		case errors.Is(serr, fs.ErrNotExist):
			// File already gone but the column still points at it: fall through
			// and clear the column so the row self-heals.
		case serr != nil:
			p.logger.Warnf("capture retention: capture %d: stat %s: %v", c.ID, abs, serr)
			continue
		default:
			p.logger.Warnf("capture retention: capture %d video_path %s is not a regular file (mode %s), refusing to delete", c.ID, abs, fi.Mode())
			continue
		}

		if uerr := p.st.SetCaptureVideoPath(c.ID, ""); uerr != nil {
			p.logger.Warnf("capture retention: capture %d: clear video_path: %v", c.ID, uerr)
			continue
		}
		rowsCleared++
	}

	if rowsCleared > 0 || filesDeleted > 0 {
		p.logger.Infof("capture retention: pruned %d video file(s) (%d bytes) across %d capture row(s) older than %d day(s); keyframes and transcripts kept",
			filesDeleted, bytesFreed, rowsCleared, days)
	} else {
		p.logger.Infof("capture retention: nothing to prune (window %d day(s), %d capture row(s) scanned)", days, len(captures))
	}
	return filesDeleted, nil
}

// retentionDays reads the window live from the settings table each sweep.
func (p *Pruner) retentionDays() int {
	v, ok := p.st.GetSetting(SettingKey)
	if !ok {
		return DefaultRetentionDays
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		p.logger.Warnf("capture retention: %s=%q is not an integer, using default %d", SettingKey, v, DefaultRetentionDays)
		return DefaultRetentionDays
	}
	return n
}

func (p *Pruner) capturesRoot() string {
	root := filepath.Join(p.dataDir, "captures")
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return filepath.Clean(root)
}

// withinRoot resolves path and reports whether it lives strictly inside root.
// Both sides are cleaned and made absolute first, so "..", relative paths and
// duplicate separators cannot escape: a corrupted or hostile video_path must
// never make the daemon delete something outside {dataDir}/captures.
func withinRoot(root, path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}
