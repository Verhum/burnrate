package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// The attack the origin guard exists to stop: a page the user visits queues a
// task, which the daemon runs as `claude --permission-mode auto`.
func TestCrossOriginTaskCreateRefused(t *testing.T) {
	s, st := testServer(t)

	body, _ := json.Marshal(map[string]string{"title": "pwned", "prompt": "exfiltrate"})
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("refused response must not carry CORS headers, got %q", got)
	}
	tasks, _ := st.ListTasks()
	if len(tasks) != 0 {
		t.Fatalf("cross-origin POST created %d task(s)", len(tasks))
	}
}

// A rebound DNS name resolves to 127.0.0.1 but keeps the attacker's Origin, so
// the same allowlist covers it.
func TestDNSRebindingOriginRefused(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Host = "127.0.0.1:9112"
	req.Header.Set("Origin", "http://rebind.evil.example")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

// Origin is omitted on same-origin GETs, so Sec-Fetch-Site is the only signal.
func TestCrossSiteReadWithoutOriginRefused(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSameOriginRequestAllowed(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The Tauri webview serves embedded assets, so every API call it makes is
// cross-origin against the sidecar. Breaking this breaks the desktop app.
func TestTauriOriginAllowed(t *testing.T) {
	s, _ := testServer(t)

	for _, origin := range []string{"tauri://localhost", "http://tauri.localhost"} {
		req := httptest.NewRequest("GET", "/api/tasks", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Fatalf("origin %s: expected 200, got %d", origin, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("origin %s: reflected %q", origin, got)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("origin %s: missing Vary: Origin", origin)
		}
	}
}

func TestLoopbackSelfOriginAllowed(t *testing.T) {
	s, _ := testServer(t)

	origin := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.Port)
	req := httptest.NewRequest("OPTIONS", "/api/tasks", nil)
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 204 {
		t.Fatalf("preflight: expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("preflight reflected %q, want %q", got, origin)
	}
}

func TestPreflightFromDisallowedOriginRefused(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/tasks", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

// The Tauri tray polls over reqwest and the dev proxy forwards server-side;
// neither sends Origin or Sec-Fetch-Site, and a browser page cannot suppress both.
func TestNonBrowserClientAllowed(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Following a link to the dashboard is a cross-site navigation and must still
// render; only the API is guarded.
func TestCrossSiteNavigationToUIAllowed(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code == 403 {
		t.Fatalf("UI navigation refused: %s", rec.Body.String())
	}
}

// The MCP endpoint is a second privileged control plane: tools/call creates
// human requests and captures against arbitrary task ids. It must be guarded
// exactly like /api/.
func TestCrossOriginMCPRefused(t *testing.T) {
	s, _ := testServer(t)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_human","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("refused response must not carry CORS headers, got %q", got)
	}
}

// The actual exploit shape. text/plain is a CORS-safelisted content type, so
// the browser sends the POST with no preflight *and* no Origin header —
// Sec-Fetch-Site is the only thing left to reject it on.
func TestCrossSiteMCPWithoutOriginRefused(t *testing.T) {
	s, _ := testServer(t)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_human","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("refused response must not carry CORS headers, got %q", got)
	}
}

// A subpath under /mcp must not fall out of the guard if one is ever added.
func TestCrossSiteMCPSubpathRefused(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("POST", "/mcp/messages", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The claude CLI talks to /mcp over plain HTTP with neither Origin nor
// Sec-Fetch-Site. Guarding the path must not lock it out.
func TestNonBrowserMCPClientAllowed(t *testing.T) {
	s, _ := testServer(t)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConfigWriteRejectsTokenExfiltrationKeys(t *testing.T) {
	s, _ := testServer(t)

	for _, key := range []string{"usage_url", "port", "base_code_dir"} {
		body, _ := json.Marshal(map[string]any{key: "http://evil.example"})
		req := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != 400 {
			t.Fatalf("key %s: expected 400, got %d: %s", key, rec.Code, rec.Body.String())
		}
	}
}
