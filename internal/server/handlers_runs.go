package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/Verhum/burnrate/internal/claude"
	"github.com/Verhum/burnrate/internal/store"
)

type logEvent struct {
	Type         string          `json:"type"`
	SessionID    string          `json:"session_id,omitempty"`
	Model        string          `json:"model,omitempty"`
	Text         string          `json:"text,omitempty"`
	ToolName     string          `json:"tool_name,omitempty"`
	InputSummary string          `json:"input_summary,omitempty"`
	InputFull    json.RawMessage `json:"input_full,omitempty"`
	Output       string          `json:"output,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	NumTurns     int             `json:"num_turns,omitempty"`
	Duration     int             `json:"duration,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	Message      string          `json:"message,omitempty"`
	Raw          string          `json:"raw,omitempty"`
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	var taskID int64
	if v := r.URL.Query().Get("task_id"); v != "" {
		var err error
		taskID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, 400, "invalid task_id")
			return
		}
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := s.runSvc.ListRuns(r.Context(), taskID, limit)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	writeJSON(w, 200, runs)
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid run id")
		return
	}
	logPath := s.runSvc.GetRunLogPath(id)
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, 404, "log not found")
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	defer f.Close()

	const maxBytes = 512 * 1024
	info, err := f.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if offset > 0 {
		f.Seek(offset, io.SeekStart)
	}
	io.Copy(w, f)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid run id")
		return
	}
	logPath := s.runSvc.GetRunLogPath(id)
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 200, []logEvent{})
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	defer f.Close()

	const maxBytes = 512 * 1024
	info, statErr := f.Stat()
	if statErr != nil {
		writeError(w, 500, statErr.Error())
		return
	}
	if info.Size() > maxBytes {
		f.Seek(info.Size()-maxBytes, io.SeekStart)
	}

	var events []logEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxBytes+64*1024)
	first := info.Size() > maxBytes
	for scanner.Scan() {
		line := scanner.Bytes()
		if first {
			first = false
			continue
		}
		if len(line) == 0 {
			continue
		}
		evt, parseErr := claude.ParseLine(line)
		if parseErr != nil {
			events = append(events, logEvent{Type: "unknown", Raw: string(line)})
			continue
		}
		switch evt.Type {
		case "system":
			if evt.Subtype == "init" || evt.SessionID != "" {
				events = append(events, logEvent{Type: "init", SessionID: evt.SessionID, Model: evt.Model})
			}
		case "assistant":
			if evt.Message != nil {
				var msg claude.AssistantMessage
				if json.Unmarshal(evt.Message, &msg) == nil {
					for _, block := range msg.Content {
						switch block.Type {
						case "text":
							if block.Text != "" {
								events = append(events, logEvent{Type: "assistant_text", Text: block.Text})
							}
						case "tool_use":
							summary := compactInput(block.Input)
							events = append(events, logEvent{
								Type:         "tool_use",
								ToolName:     block.Name,
								InputSummary: summary,
								InputFull:    block.Input,
							})
						case "tool_result":
							preview := truncateContent(block.Content)
							events = append(events, logEvent{Type: "tool_result", Output: preview})
						}
					}
				}
			}
			if claude.IsRateLimitMessage(evt.Result) {
				events = append(events, logEvent{Type: "rate_limit", Message: evt.Result})
			}
		case "user":
			if evt.Message != nil {
				var msg claude.AssistantMessage
				if json.Unmarshal(evt.Message, &msg) == nil {
					for _, block := range msg.Content {
						if block.Type == "tool_result" {
							preview := truncateContent(block.Content)
							events = append(events, logEvent{Type: "tool_result", Output: preview, ToolName: block.ToolUseID})
						}
					}
				}
			}
		case "result":
			le := logEvent{
				Type:     "result",
				CostUSD:  evt.TotalCost,
				NumTurns: evt.NumTurns,
				Duration: evt.DurationMS,
				IsError:  evt.IsError,
			}
			if claude.IsRateLimitMessage(evt.Result) {
				le.Type = "rate_limit"
				le.Message = evt.Result
			} else if evt.Result != "" {
				le.Text = evt.Result
			}
			events = append(events, le)
		default:
			if claude.IsRateLimitMessage(evt.Result) {
				events = append(events, logEvent{Type: "rate_limit", Message: evt.Result})
			} else {
				events = append(events, logEvent{Type: "unknown", Raw: string(line)})
			}
		}
	}
	if events == nil {
		events = []logEvent{}
	}
	writeJSON(w, 200, events)
}

func (s *Server) handleRunResume(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid run id")
		return
	}
	info, err := s.runSvc.ResumeInfo(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, 200, info)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "invalid run id")
		return
	}
	result, err := s.runSvc.CancelRun(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}
	if result == "cancelled" {
		s.broadcastRuns()
	}
	s.broadcastTasks()
	s.hub.broadcast("status", s.statusPayload())
	writeJSON(w, 200, map[string]string{"status": result})
}
