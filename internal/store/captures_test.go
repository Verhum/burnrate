package store

import (
	"testing"
)

func TestCaptureCRUD(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("test task", "do something", "", "medium", "", "queued")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	cap, err := st.CreateCapture(task.ID, 0, "agent", "display:0", "screenshot")
	if err != nil {
		t.Fatalf("create capture: %v", err)
	}
	if cap.Initiator != "agent" || cap.Status != "processing" || cap.Mode != "screenshot" {
		t.Fatalf("unexpected capture: %+v", cap)
	}

	got, err := st.GetCapture(cap.ID)
	if err != nil {
		t.Fatalf("get capture: %v", err)
	}
	if got.TaskID != task.ID || got.TargetDesc != "display:0" {
		t.Fatalf("get mismatch: %+v", got)
	}

	caps, err := st.ListCaptures(task.ID)
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(caps))
	}
}

func TestCaptureFinish(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	cap, _ := st.CreateCapture(task.ID, 0, "human", "window:Terminal", "video")

	if err := st.FinishCapture(cap.ID, "/tmp/video.mp4", "User said it looks good", 30.5); err != nil {
		t.Fatalf("finish capture: %v", err)
	}

	got, _ := st.GetCapture(cap.ID)
	if got.Status != "ready" {
		t.Fatalf("expected ready, got %s", got.Status)
	}
	if got.VideoPath != "/tmp/video.mp4" {
		t.Fatalf("expected video path, got %s", got.VideoPath)
	}
	if got.Transcript != "User said it looks good" {
		t.Fatalf("expected transcript, got %s", got.Transcript)
	}
	if got.DurationSec != 30.5 {
		t.Fatalf("expected 30.5s, got %f", got.DurationSec)
	}
}

func TestCaptureNotes(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	cap, _ := st.CreateCapture(task.ID, 0, "human", "display:0", "screenshot")

	if err := st.SetCaptureNotes(cap.ID, "The modal is misaligned"); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	got, _ := st.GetCapture(cap.ID)
	if got.Notes != "The modal is misaligned" {
		t.Fatalf("expected notes, got %s", got.Notes)
	}
}

func TestCaptureStatusTransition(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("test", "prompt", "", "medium", "", "queued")
	cap, _ := st.CreateCapture(task.ID, 0, "agent", "display:0", "screenshot")

	if cap.Status != "processing" {
		t.Fatalf("expected processing, got %s", cap.Status)
	}

	if err := st.SetCaptureStatus(cap.ID, "failed"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	got, _ := st.GetCapture(cap.ID)
	if got.Status != "failed" {
		t.Fatalf("expected failed, got %s", got.Status)
	}
}

func TestListCaptures_AllTasks(t *testing.T) {
	st := testStore(t)

	t1, _ := st.CreateTask("task1", "p1", "", "medium", "", "queued")
	t2, _ := st.CreateTask("task2", "p2", "", "medium", "", "queued")
	st.CreateCapture(t1.ID, 0, "agent", "d:0", "screenshot")
	st.CreateCapture(t2.ID, 0, "human", "d:0", "video")

	all, err := st.ListCaptures(0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	forT1, _ := st.ListCaptures(t1.ID)
	if len(forT1) != 1 {
		t.Fatalf("expected 1 for task1, got %d", len(forT1))
	}
}
