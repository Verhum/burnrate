package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Verhum/burnrate/internal/store"
)

func resumeService(t *testing.T, configDir string) (*RunService, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := NewRunService(st, st, newMockScheduler(), RunServiceConfig{
		DataDir:         t.TempDir(),
		ClaudeConfigDir: configDir,
		TokenCommand:    "/opt/burnrate token",
	})
	return svc, st
}

// A pinned account writes its session transcripts under CLAUDE_CONFIG_DIR, so a
// resume command that omits the assignment sends the user to a terminal that
// reports the session as missing.
func TestResumeInfo_CarriesPinnedConfigDir(t *testing.T) {
	svc, st := resumeService(t, "/base/code/proj/.local_home")

	task, _ := st.CreateTask("t", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "/work/task-1", "b", "", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-abc")

	info, err := svc.ResumeInfo(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ResumeInfo: %v", err)
	}
	want := `cd '/work/task-1' && CLAUDE_CONFIG_DIR='/base/code/proj/.local_home' CLAUDE_CODE_OAUTH_TOKEN="$(/opt/burnrate token)" claude --resume 'sess-abc'`
	if info.Command != want {
		t.Errorf("command\n got %q\nwant %q", info.Command, want)
	}
	if info.SessionID != "sess-abc" || info.WorktreePath != "/work/task-1" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestResumeInfo_InheritedAccountOmitsConfigDir(t *testing.T) {
	svc, st := resumeService(t, "")

	task, _ := st.CreateTask("t", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "/work/task-1", "b", "", "w1", 1)
	st.SetRunSessionID(run.ID, "sess-abc")

	info, err := svc.ResumeInfo(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ResumeInfo: %v", err)
	}
	want := "cd '/work/task-1' && claude --resume 'sess-abc'"
	if info.Command != want {
		t.Errorf("command\n got %q\nwant %q", info.Command, want)
	}
}

func TestResumeInfo_UnknownRun(t *testing.T) {
	svc, _ := resumeService(t, "")

	if _, err := svc.ResumeInfo(context.Background(), 4242); err == nil {
		t.Fatal("expected a not-found error for an unknown run")
	}
}
