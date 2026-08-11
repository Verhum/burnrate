package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Verhum/burnrate/internal/service"
	"github.com/Verhum/burnrate/internal/store"
)

func TestInitialize(t *testing.T) {
	srv := New(nil, nil)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp?task=1&run=1", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("unexpected protocol version: %v", result["protocolVersion"])
	}
}

func TestToolsList(t *testing.T) {
	srv := New(nil, nil)

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp?task=1&run=1", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, _ := resp.Result.(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools is not a slice: %T", result["tools"])
	}
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		m := tool.(map[string]any)
		toolNames[m["name"].(string)] = true
	}
	for _, name := range []string{"ask_human", "await_request", "request_demo", "list_capture_targets", "capture_screen"} {
		if !toolNames[name] {
			t.Fatalf("missing %s tool", name)
		}
	}
}

func TestPing(t *testing.T) {
	srv := New(nil, nil)

	body := `{"jsonrpc":"2.0","id":3,"method":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := New(nil, nil)

	body := `{"jsonrpc":"2.0","id":4,"method":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// H17 — the capture tools are honest about not existing
// ---------------------------------------------------------------------------

func callTool(t *testing.T, srv *Server, name, args string, query string) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	req := httptest.NewRequest(http.MethodPost, "/mcp"+query, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp jsonRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	return result
}

func resultText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in %+v", result)
	}
	return content[0].(map[string]any)["text"].(string)
}

// Desktop capture does not exist. Returning a fabricated 1920x1080 display, or
// a capture row that stays `processing` forever, sent the agent chasing
// something that could never happen.
func TestCaptureToolsRefuseHonestly(t *testing.T) {
	srv := New(nil, nil)

	for _, tc := range []struct{ name, args string }{
		{"list_capture_targets", `{}`},
		{"capture_screen", `{"target":{"display":0}}`},
	} {
		result := callTool(t, srv, tc.name, tc.args, "?task=1&run=1")

		if isErr, _ := result["isError"].(bool); !isErr {
			t.Fatalf("%s: must report isError:true, got %+v", tc.name, result)
		}
		text := resultText(t, result)
		if !strings.Contains(text, "desktop app") {
			t.Fatalf("%s: message should say why it cannot work, got %q", tc.name, text)
		}
		// The old stub fabricated a display; nothing should look like success.
		if strings.Contains(text, "1920") {
			t.Fatalf("%s: must not fabricate a display, got %q", tc.name, text)
		}
		if strings.Contains(text, "pending_capture") {
			t.Fatalf("%s: must not claim a capture is pending, got %q", tc.name, text)
		}
	}
}

// The tool descriptions must not advertise capture as usable either — the
// agent picks tools off the list before it ever calls one.
func TestCaptureToolDescriptionsSayNotImplemented(t *testing.T) {
	srv := New(nil, nil)
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp jsonRPCResponse
	json.NewDecoder(w.Body).Decode(&resp)
	result, _ := resp.Result.(map[string]any)
	tools, _ := result["tools"].([]any)

	for _, tool := range tools {
		m := tool.(map[string]any)
		name := m["name"].(string)
		if name != "list_capture_targets" && name != "capture_screen" {
			continue
		}
		if !strings.Contains(m["description"].(string), "NOT IMPLEMENTED") {
			t.Fatalf("%s description must say it is not implemented, got %q", name, m["description"])
		}
	}
}

// ---------------------------------------------------------------------------
// F13 — await_request ownership
// ---------------------------------------------------------------------------

func ownershipServer(t *testing.T) (*Server, int64, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	mine, _ := st.CreateTask("mine", "p", "", "medium", "", "queued")
	theirs, _ := st.CreateTask("theirs", "p", "", "medium", "", "queued")

	svc := service.NewRequestService(st, st, st, st, st, nil)
	// A request belonging to the OTHER task.
	req, err := svc.Create(context.Background(), theirs.ID, 0, "question", "secret", "secret")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	return New(svc, nil), mine.ID, req.ID
}

// The request id is agent-supplied; the task id comes from the URL burnrate
// minted. Without the check, an agent could await another task's request and
// flip its `live` flag, reordering the human's queue.
func TestAwaitRequestRejectsForeignRequest(t *testing.T) {
	srv, myTask, foreignReq := ownershipServer(t)

	result := callTool(t, srv, "await_request",
		fmt.Sprintf(`{"request_id":%d,"wait_sec":5}`, foreignReq),
		fmt.Sprintf("?task=%d&run=1", myTask))

	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true, got %+v", result)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "does not belong to task") {
		t.Fatalf("expected an ownership refusal, got %q", text)
	}
}

func TestAwaitRequestRejectsUnknownRequest(t *testing.T) {
	srv, myTask, _ := ownershipServer(t)

	result := callTool(t, srv, "await_request",
		`{"request_id":424242,"wait_sec":5}`,
		fmt.Sprintf("?task=%d&run=1", myTask))

	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true, got %+v", result)
	}
	if text := resultText(t, result); !strings.Contains(text, "not found") {
		t.Fatalf("expected a not-found refusal, got %q", text)
	}
}
