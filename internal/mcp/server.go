package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Verhum/burnrate/internal/service"
)

type Server struct {
	requestSvc *service.RequestService
	captureSvc *service.CaptureService
}

func New(requestSvc *service.RequestService, captureSvc *service.CaptureService) *Server {
	return &Server{requestSvc: requestSvc, captureSvc: captureSvc}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// SSE transport not implemented; return endpoint info.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: endpoint\ndata: {\"url\":\"/mcp\"}\n\n")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID, _ := strconv.ParseInt(r.URL.Query().Get("task"), 10, 64)
	runID, _ := strconv.ParseInt(r.URL.Query().Get("run"), 10, 64)

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, nil, nil, &jsonRPCError{Code: -32700, Message: "parse error"})
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req, taskID, runID)
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(w, r, req, taskID, runID)
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	case "ping":
		writeJSONRPC(w, req.ID, map[string]any{}, nil)
	default:
		writeJSONRPC(w, req.ID, nil, &jsonRPCError{Code: -32601, Message: "method not found"})
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, req jsonRPCRequest, _, _ int64) {
	writeJSONRPC(w, req.ID, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "burnrate-human-loop",
			"version": "1.0.0",
		},
	}, nil)
}

var toolDefinitions = []map[string]any{
	{
		"name":        "ask_human",
		"description": "Ask the human operator a question. Blocks until the human replies or the wait budget expires, then returns the answer or parks the task.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The question to ask (markdown).",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Additional context for the human (markdown, optional).",
				},
				"wait_sec": map[string]any{
					"type":        "integer",
					"description": "How long to wait for a reply (default: server config human_request_wait_sec).",
				},
			},
			"required": []string{"question"},
		},
	},
	{
		"name":        "await_request",
		"description": "Re-attach to a previously created request and wait for its response.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{
					"type":        "integer",
					"description": "The ID of the request to await.",
				},
				"wait_sec": map[string]any{
					"type":        "integer",
					"description": "How long to wait (default: 55s).",
				},
			},
			"required": []string{"request_id"},
		},
	},
	{
		"name":        "request_demo",
		"description": "Ask the human to run a visual test and record it. The human records screen + voice, and burnrate processes it into keyframes + transcript. Blocks until the human submits or the wait budget expires.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "Short title for the demo request.",
				},
				"steps": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Steps for the human to follow.",
				},
				"expected": map[string]any{
					"type":        "string",
					"description": "What the correct result should look like.",
				},
				"look_for": map[string]any{
					"type":        "string",
					"description": "Specific things to check or look for.",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "URL to open for the test.",
				},
				"revival_steps": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd":     map[string]any{"type": "string"},
						"command": map[string]any{"type": "string"},
						"port":    map[string]any{"type": "integer"},
					},
					"description": "How to restart the server if dead.",
				},
				"wait_sec": map[string]any{
					"type":        "integer",
					"description": "How long to wait for the demo (default: server config human_request_wait_sec).",
				},
			},
			"required": []string{"title", "steps"},
		},
	},
	{
		"name":        "list_capture_targets",
		"description": "NOT IMPLEMENTED. Screen capture requires the burnrate desktop app, which does not implement capture yet. This tool always fails — use request_demo to have the human record a screen+voice demo instead.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		"name":        "capture_screen",
		"description": "NOT IMPLEMENTED. Screen capture requires the burnrate desktop app, which does not implement capture yet. This tool always fails — use request_demo (human records the screen) or ask_human instead.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"window_title": map[string]any{
							"type":        "string",
							"description": "Match a window by title substring.",
						},
						"display": map[string]any{
							"type":        "integer",
							"description": "Display index (0 = main).",
						},
						"region": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "integer"},
							"description": "[x, y, width, height] region on the display.",
						},
					},
					"description": "What to capture.",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"screenshot", "video"},
					"description": "Capture mode (default: screenshot).",
				},
				"duration_sec": map[string]any{
					"type":        "integer",
					"description": "Video duration in seconds (max 60, only for video mode).",
				},
				"note": map[string]any{
					"type":        "string",
					"description": "Why you want this capture — shown to the human in the approval prompt.",
				},
			},
			"required": []string{"target"},
		},
	},
}

func (s *Server) handleToolsList(w http.ResponseWriter, req jsonRPCRequest) {
	writeJSONRPC(w, req.ID, map[string]any{
		"tools": toolDefinitions,
	}, nil)
}

func (s *Server) handleToolsCall(w http.ResponseWriter, r *http.Request, req jsonRPCRequest, taskID, runID int64) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPC(w, req.ID, nil, &jsonRPCError{Code: -32602, Message: "invalid params"})
		return
	}

	switch params.Name {
	case "ask_human":
		s.callAskHuman(w, r.Context(), req.ID, params.Arguments, taskID, runID)
	case "await_request":
		s.callAwaitRequest(w, r.Context(), req.ID, params.Arguments, taskID)
	case "request_demo":
		s.callRequestDemo(w, r.Context(), req.ID, params.Arguments, taskID, runID)
	case "list_capture_targets", "capture_screen":
		writeJSONRPC(w, req.ID, toolResult(true, captureUnavailableMsg), nil)
	default:
		writeJSONRPC(w, req.ID, toolResult(true, fmt.Sprintf("unknown tool: %s", params.Name)), nil)
	}
}

// captureUnavailableMsg is returned for both capture tools. Nothing in
// desktop/ implements capture, so the old behaviour — fabricating a 1920x1080
// display for list_capture_targets, and creating an approval request plus a
// capture row that stayed `processing` forever for capture_screen — lied to
// the agent and interrupted the human for work that could never run. An honest
// error costs one turn; a fabricated success costs the whole task.
const captureUnavailableMsg = `{"status":"unavailable","error":"screen capture requires the burnrate desktop app, which does not implement capture yet. No approval request was created and nothing was captured. Use request_demo (ask the human to record a screen+voice demo) or ask_human instead."}`

func (s *Server) callAskHuman(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage, taskID, runID int64) {
	var input struct {
		Question string `json:"question"`
		Context  string `json:"context"`
		WaitSec  int    `json:"wait_sec"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeJSONRPC(w, id, toolResult(true, "invalid arguments"), nil)
		return
	}

	body := input.Question
	if input.Context != "" {
		body += "\n\n---\n\n" + input.Context
	}

	req, err := s.requestSvc.Create(ctx, taskID, runID, "question", input.Question, body)
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("failed to create request: %v", err)), nil)
		return
	}

	waitSec := service.ClampWait(input.WaitSec, s.requestSvc.HumanRequestWaitSec())

	out, err := s.requestSvc.AwaitResponse(ctx, req.ID, waitSec)
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("await error: %v", err)), nil)
		return
	}

	writeJSONRPC(w, id, awaitToolResult(out), nil)
}

// awaitToolResult turns a settled long-poll into the tool result the agent
// sees. The answered case carries the human's actual words: the agent has no
// tool for reading the comment thread, so the old "Response posted as comment
// #N, check the thread" was functionally a dropped message.
func awaitToolResult(out *service.AwaitOutcome) map[string]any {
	req := out.Request
	payload := map[string]any{"request_id": req.ID}

	switch {
	case req.Status == "answered":
		payload["status"] = "answered"
		payload["reply"] = out.Reply
		if out.Result != "" {
			payload["result"] = out.Result
		}
		if out.Reply == "" {
			// Approved without a written reply (POST /api/requests/{id}/approve).
			payload["reply"] = "The human approved this request without a written reply."
		}
	case req.Status == "denied":
		payload["status"] = "denied"
		payload["reason"] = "The human denied this request."
	case req.Status == "canceled":
		payload["status"] = "canceled"
		payload["reason"] = "The task was completed or dismissed while this request was open. Stop waiting and finish up."
	case req.Status == "expired":
		payload["status"] = "timeout"
		payload["reason"] = "The wait budget expired and this request is no longer answerable."
	default:
		// Still pending: the budget ran out but the human can still answer, so
		// the task parks rather than the request dying.
		payload["status"] = "parked"
		payload["instructions"] = "The human has not replied yet. Post a state summary as a comment, then end this run with the trailer RESULT: WAITING_HUMAN. The task will be parked until the human responds, and the reply will reach you on the next run."
	}
	return toolResultJSON(payload)
}

func (s *Server) callAwaitRequest(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage, taskID int64) {
	var input struct {
		RequestID int64 `json:"request_id"`
		WaitSec   int   `json:"wait_sec"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeJSONRPC(w, id, toolResult(true, "invalid arguments"), nil)
		return
	}

	// Ownership check. The request id is agent-supplied while the task id comes
	// from the URL burnrate itself minted for this run, so without this an agent
	// could await another task's request — and flip its `live` flag on, which
	// reorders the human's queue.
	req, err := s.requestSvc.Get(ctx, input.RequestID)
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("request %d not found", input.RequestID)), nil)
		return
	}
	if req.TaskID != taskID {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("request %d does not belong to task %d", input.RequestID, taskID)), nil)
		return
	}

	waitSec := service.ClampWait(input.WaitSec, s.requestSvc.HumanRequestWaitSec())

	out, err := s.requestSvc.AwaitResponse(ctx, input.RequestID, waitSec)
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("await error: %v", err)), nil)
		return
	}

	writeJSONRPC(w, id, awaitToolResult(out), nil)
}

func (s *Server) callRequestDemo(w http.ResponseWriter, ctx context.Context, id json.RawMessage, args json.RawMessage, taskID, runID int64) {
	var input struct {
		Title        string   `json:"title"`
		Steps        []string `json:"steps"`
		Expected     string   `json:"expected"`
		LookFor      string   `json:"look_for"`
		URL          string   `json:"url"`
		RevivalSteps any      `json:"revival_steps"`
		WaitSec      int      `json:"wait_sec"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		writeJSONRPC(w, id, toolResult(true, "invalid arguments"), nil)
		return
	}

	bodyMap := map[string]any{
		"steps":    input.Steps,
		"expected": input.Expected,
		"look_for": input.LookFor,
		"url":      input.URL,
	}
	if input.RevivalSteps != nil {
		bodyMap["revival_steps"] = input.RevivalSteps
	}
	bodyJSON, _ := json.Marshal(bodyMap)

	req, err := s.requestSvc.Create(ctx, taskID, runID, "demo", input.Title, string(bodyJSON))
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("failed to create demo request: %v", err)), nil)
		return
	}

	waitSec := service.ClampWait(input.WaitSec, s.requestSvc.HumanRequestWaitSec())

	out, err := s.requestSvc.AwaitResponse(ctx, req.ID, waitSec)
	if err != nil {
		writeJSONRPC(w, id, toolResult(true, fmt.Sprintf("await error: %v", err)), nil)
		return
	}

	writeJSONRPC(w, id, awaitToolResult(out), nil)
}

// toolResultJSON is the success shape for the human-loop tools: a JSON object
// serialised into the single text content block MCP gives us.
func toolResultJSON(payload map[string]any) map[string]any {
	data, err := json.Marshal(payload)
	if err != nil {
		return toolResult(true, fmt.Sprintf("failed to encode tool result: %v", err))
	}
	return toolResult(false, string(data))
}

func toolResult(isError bool, text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

func writeJSONRPC(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *jsonRPCError) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
