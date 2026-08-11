package store

import "testing"

func TestTaskPRsMultipleRepos(t *testing.T) {
	st := testStore(t)

	task, err := st.CreateTask("multi-repo task", "touch two repos", "", "medium", "", "")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	run, err := st.CreateRun(task.ID, "/wt", "", "", "win-1", 1)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := st.UpsertTaskPR(task.ID, run.ID, "acme/api", "burnrate/1-x", "https://github.com/acme/api/pull/1", "/wt/api"); err != nil {
		t.Fatalf("upsert api: %v", err)
	}
	if err := st.UpsertTaskPR(task.ID, run.ID, "acme/web", "burnrate/1-x", "https://github.com/acme/web/pull/2", "/wt/web"); err != nil {
		t.Fatalf("upsert web: %v", err)
	}

	prs, err := st.ListTaskPRs(task.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d: %+v", len(prs), prs)
	}
	if prs[0].Repo != "acme/api" || prs[1].Repo != "acme/web" {
		t.Fatalf("unexpected PR order/content: %+v", prs)
	}

	// A resumed run re-reporting the same repo+branch updates in place.
	run2, err := st.CreateRun(task.ID, "/wt", "", "", "win-2", 2)
	if err != nil {
		t.Fatalf("create run 2: %v", err)
	}
	if err := st.UpsertTaskPR(task.ID, run2.ID, "acme/api", "burnrate/1-x", "https://github.com/acme/api/pull/1", "/wt/api"); err != nil {
		t.Fatalf("re-upsert api: %v", err)
	}
	prs, _ = st.ListTaskPRs(task.ID)
	if len(prs) != 2 {
		t.Fatalf("re-report should not duplicate, got %d: %+v", len(prs), prs)
	}
	if prs[0].RunID != run2.ID {
		t.Fatalf("expected run id to advance to %d, got %d", run2.ID, prs[0].RunID)
	}

	// An empty PR URL must not blank out a URL already recorded.
	if err := st.UpsertTaskPR(task.ID, run2.ID, "acme/api", "burnrate/1-x", "", "/wt/api"); err != nil {
		t.Fatalf("upsert without url: %v", err)
	}
	prs, _ = st.ListTaskPRs(task.ID)
	if prs[0].PRURL != "https://github.com/acme/api/pull/1" {
		t.Fatalf("existing PR URL was clobbered: %+v", prs[0])
	}
}

func TestListTasksAttachesPRs(t *testing.T) {
	st := testStore(t)

	task, _ := st.CreateTask("t", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "", "", "win-1", 1)
	st.UpsertTaskPR(task.ID, run.ID, "acme/api", "b1", "https://github.com/acme/api/pull/1", "/wt/api")
	st.UpsertTaskPR(task.ID, run.ID, "acme/web", "b1", "https://github.com/acme/web/pull/2", "/wt/web")

	tasks, err := st.ListTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || len(tasks[0].PRs) != 2 {
		t.Fatalf("expected 1 task with 2 PRs, got %+v", tasks)
	}

	got, err := st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if len(got.PRs) != 2 {
		t.Fatalf("expected 2 PRs on GetTask, got %+v", got.PRs)
	}
}

func TestUpsertTaskPRRejectsNonURL(t *testing.T) {
	st := testStore(t)
	task, _ := st.CreateTask("t", "p", "", "medium", "", "")
	run, _ := st.CreateRun(task.ID, "/wt", "", "", "win-1", 1)

	// Agents occasionally write prose into the PR trailer.
	if err := st.UpsertTaskPR(task.ID, run.ID, "acme/api", "b1", "none (GitHub API was down)", "/wt/api"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	prs, _ := st.ListTaskPRs(task.ID)
	if len(prs) != 1 {
		t.Fatalf("expected the repo to still be recorded, got %+v", prs)
	}
	if prs[0].PRURL != "" {
		t.Fatalf("non-URL should be stored as empty, got %q", prs[0].PRURL)
	}
}
