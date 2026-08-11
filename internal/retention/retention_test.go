package retention

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
	brlog "github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/store"
)

// fixture builds a real store in a temp data dir. The pruner's clock is
// injectable, so tests age captures by moving the clock forward rather than
// rewriting created_at behind the store's back.
func fixture(t *testing.T) (*Pruner, *store.Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	p := New(st, dataDir, brlog.New("", false))
	return p, st, dataDir
}

// ageBy makes every existing capture look ageDays old to the pruner.
func ageBy(p *Pruner, ageDays int) {
	at := time.Now().UTC().AddDate(0, 0, ageDays)
	p.now = func() time.Time { return at }
}

// newCapture inserts a finished capture with a video file and a keyframe
// alongside it. Returns the capture id, the video path and the keyframe path.
func newCapture(t *testing.T, st *store.Store, dataDir string) (int64, string, string) {
	t.Helper()
	task, err := st.CreateTask("t", "p", "", "medium", "", "queued")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	cap, err := st.CreateCapture(task.ID, 0, "human", "display", "video")
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}

	dir := filepath.Join(dataDir, "captures", fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("capture-%d", cap.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	video := filepath.Join(dir, "recording.mp4")
	if err := os.WriteFile(video, []byte("video-bytes"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	keyframe := filepath.Join(dir, "frame-001.png")
	if err := os.WriteFile(keyframe, []byte("png"), 0o644); err != nil {
		t.Fatalf("write keyframe: %v", err)
	}

	if err := st.FinishCapture(cap.ID, video, "spoken transcript", 12); err != nil {
		t.Fatalf("finish capture: %v", err)
	}
	return cap.ID, video, keyframe
}

// fakeStore drives the cases a real store cannot easily produce.
type fakeStore struct {
	captures []domain.Capture
	setting  string
	hasSet   bool
	cleared  []int64
}

func (f *fakeStore) ListCaptures(taskID int64) ([]domain.Capture, error) { return f.captures, nil }
func (f *fakeStore) SetCaptureVideoPath(id int64, path string) error {
	f.cleared = append(f.cleared, id)
	return nil
}
func (f *fakeStore) GetSetting(key string) (string, bool) { return f.setting, f.hasSet }

func TestSweepDeletesAgedVideoAndKeepsKeyframes(t *testing.T) {
	p, st, dataDir := fixture(t)
	id, video, keyframe := newCapture(t, st, dataDir)
	ageBy(p, 60)

	n, err := p.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 file deleted, got %d", n)
	}
	if _, err := os.Stat(video); !os.IsNotExist(err) {
		t.Fatalf("video should be gone, stat err = %v", err)
	}
	// The whole point of the sweep: the cheap durable record survives.
	if _, err := os.Stat(keyframe); err != nil {
		t.Fatalf("keyframe must survive pruning: %v", err)
	}
	cap, err := st.GetCapture(id)
	if err != nil {
		t.Fatalf("get capture: %v", err)
	}
	if cap.VideoPath != "" {
		t.Fatalf("video_path should be cleared, got %q", cap.VideoPath)
	}
	if cap.Transcript != "spoken transcript" {
		t.Fatalf("transcript must survive pruning, got %q", cap.Transcript)
	}
}

func TestSweepKeepsRecentVideo(t *testing.T) {
	p, st, dataDir := fixture(t)
	id, video, _ := newCapture(t, st, dataDir)
	ageBy(p, 3)

	n, err := p.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected nothing deleted, got %d", n)
	}
	if _, err := os.Stat(video); err != nil {
		t.Fatalf("recent video must survive: %v", err)
	}
	cap, _ := st.GetCapture(id)
	if cap.VideoPath == "" {
		t.Fatal("recent capture's video_path must not be cleared")
	}
}

// N<=0 means "keep forever" — a user who turns retention off must not lose
// anything on the next daily tick.
func TestSweepDisabledWhenRetentionNotPositive(t *testing.T) {
	for _, v := range []string{"0", "-1"} {
		p, st, dataDir := fixture(t)
		_, video, _ := newCapture(t, st, dataDir)
		ageBy(p, 9999)
		if err := st.SetSetting(SettingKey, v); err != nil {
			t.Fatalf("set setting: %v", err)
		}

		n, err := p.SweepOnce(context.Background())
		if err != nil {
			t.Fatalf("%s: sweep: %v", v, err)
		}
		if n != 0 {
			t.Fatalf("%s: expected nothing deleted, got %d", v, n)
		}
		if _, err := os.Stat(video); err != nil {
			t.Fatalf("%s: video must survive with retention disabled: %v", v, err)
		}
	}
}

func TestSweepHonoursConfiguredWindow(t *testing.T) {
	p, st, dataDir := fixture(t)
	_, video, _ := newCapture(t, st, dataDir)
	ageBy(p, 10)

	// Default is 30 days, so a 10-day-old capture survives...
	if n, _ := p.SweepOnce(context.Background()); n != 0 {
		t.Fatalf("expected survival under the default window, deleted %d", n)
	}
	if _, err := os.Stat(video); err != nil {
		t.Fatalf("video should still exist: %v", err)
	}

	// ...until the window is narrowed. Read live, no restart.
	if err := st.SetSetting(SettingKey, "7"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if n, _ := p.SweepOnce(context.Background()); n != 1 {
		t.Fatalf("expected the video to be pruned at a 7-day window, deleted %d", n)
	}
}

// A corrupted or hostile video_path must never make the daemon delete
// something outside {dataDir}/captures.
func TestSweepRefusesPathOutsideCapturesRoot(t *testing.T) {
	p, st, dataDir := fixture(t)
	id, _, _ := newCapture(t, st, dataDir)
	ageBy(p, 60)

	outside := filepath.Join(t.TempDir(), "precious.txt")
	if err := os.WriteFile(outside, []byte("do not delete me"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := st.SetCaptureVideoPath(id, outside); err != nil {
		t.Fatalf("set video path: %v", err)
	}

	n, err := p.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected refusal, deleted %d", n)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the captures root must survive: %v", err)
	}
	cap, _ := st.GetCapture(id)
	if cap.VideoPath != outside {
		t.Fatalf("a refused path must be left alone, got %q", cap.VideoPath)
	}
}

// Traversal must not smuggle a delete past the prefix check.
func TestSweepRefusesTraversalPath(t *testing.T) {
	p, st, dataDir := fixture(t)
	id, _, _ := newCapture(t, st, dataDir)
	ageBy(p, 60)

	victim := filepath.Join(dataDir, "burnrate.db")
	if err := os.WriteFile(victim, []byte("db"), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	if err := st.SetCaptureVideoPath(id, filepath.Join(dataDir, "captures", "..", "burnrate.db")); err != nil {
		t.Fatalf("set video path: %v", err)
	}

	if n, _ := p.SweepOnce(context.Background()); n != 0 {
		t.Fatalf("traversal must be refused, deleted %d", n)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("traversal target must survive: %v", err)
	}
}

// The file is gone but the column still points at it: clear the column so the
// row stops advertising a video that does not exist.
func TestSweepClearsColumnWhenFileAlreadyGone(t *testing.T) {
	p, st, dataDir := fixture(t)
	id, video, _ := newCapture(t, st, dataDir)
	ageBy(p, 60)
	if err := os.Remove(video); err != nil {
		t.Fatalf("remove video: %v", err)
	}

	n, err := p.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("no file existed to delete, got %d", n)
	}
	cap, _ := st.GetCapture(id)
	if cap.VideoPath != "" {
		t.Fatalf("video_path should self-heal to empty, got %q", cap.VideoPath)
	}
}

// An unreadable timestamp must never be treated as ancient.
func TestSweepSkipsUnparseableCreatedAt(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "captures", "task-1", "capture-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	video := filepath.Join(dir, "recording.mp4")
	if err := os.WriteFile(video, []byte("v"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	f := &fakeStore{captures: []domain.Capture{
		{ID: 1, TaskID: 1, VideoPath: video, CreatedAt: "not-a-timestamp"},
	}}
	p := New(f, dataDir, brlog.New("", false))

	n, err := p.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected skip, deleted %d", n)
	}
	if _, err := os.Stat(video); err != nil {
		t.Fatalf("video must survive an unparseable timestamp: %v", err)
	}
	if len(f.cleared) != 0 {
		t.Fatalf("a skipped row must not have its column cleared, cleared %v", f.cleared)
	}
}

func TestStartStopsOnContextCancel(t *testing.T) {
	p, st, dataDir := fixture(t)
	newCapture(t, st, dataDir)
	ageBy(p, 60)
	p.Interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}
