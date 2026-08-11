package server

import (
	"fmt"
	"net/http"

	"github.com/Verhum/burnrate/internal/checkout"
	"github.com/Verhum/burnrate/internal/store"
)

// handleCheckoutTask switches the task's branches into the user's own clones.
// Per-repo outcomes are returned with a 200 even when some failed: a task
// spanning three repos where one clone is dirty still checked out the other two,
// and collapsing that to an error would hide it.
func (s *Server) handleCheckoutTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	prs, err := s.st.ListTaskPRs(id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	results := checkout.Task(r.Context(), s.sched.Config().BaseCodeDir, prs)
	if results == nil {
		results = []checkout.Result{}
	}
	ok := false
	for _, cr := range results {
		if cr.Status == checkout.StatusCheckedOut || cr.Status == checkout.StatusAlready {
			ok = true
			break
		}
	}
	if ok {
		_ = s.st.SetSetting("checked_out_task_id", fmt.Sprintf("%d", id))
	}
	writeJSON(w, 200, results)
}

func (s *Server) handleRefreshTaskPRs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid task id")
		return
	}
	prs, err := s.prober.RefreshTask(r.Context(), id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.broadcastTasks()
	if prs == nil {
		prs = []store.TaskPR{}
	}
	writeJSON(w, 200, prs)
}
