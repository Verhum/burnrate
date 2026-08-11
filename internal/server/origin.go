package server

import (
	"fmt"
	"net/http"
	"strings"
)

// Loopback binding is not a trust boundary. Any page in the user's browser can
// reach 127.0.0.1, and this API is a privileged control plane: POST
// /api/tasks/{id}/run-now launches `claude --permission-mode auto` with
// the user's OAuth token in the child environment. So the origin allowlist below
// *is* the boundary, and rejecting a cross-site request is what stops a visited
// web page from queueing a task.
//
// It also closes DNS rebinding: a rebound host still carries the attacker's
// Origin, so it never matches.
//
// The allowlist has exactly two kinds of legitimate member. The embedded UI
// shipped inside the Go binary is same-origin and needs no entry, and the
// Next.js dev server proxies /api server-side (web/next.config.ts), so those
// requests arrive with no Origin at all.
func allowedOrigins(port int) map[string]bool {
	return map[string]bool{
		// The Tauri webview serves embedded assets, so it is genuinely
		// cross-origin against the sidecar on 127.0.0.1. Scheme varies by
		// platform and Tauri version.
		"tauri://localhost":       true,
		"http://tauri.localhost":  true,
		"https://tauri.localhost": true,

		// A browser pointed straight at the daemon. Same-origin requests omit
		// Origin, but EventSource and preflights send it.
		fmt.Sprintf("http://127.0.0.1:%d", port): true,
		fmt.Sprintf("http://localhost:%d", port): true,
	}
}

// guarded reports whether the origin check applies to this path. The API, the
// health probe, and the MCP endpoint are guarded; the static UI handler at "/"
// is not, because it must keep serving top-level navigations, which
// legitimately arrive cross-site when someone follows a link to the dashboard.
//
// /mcp needs the guard as much as /api/ does: its tools/call method creates
// human requests and captures, and a JSON-RPC POST with a CORS-safelisted
// Content-Type (text/plain) triggers no preflight, so a visited page can reach
// it directly. Both the exact path and any future sub-route are covered.
func guarded(path string) bool {
	return strings.HasPrefix(path, "/api/") ||
		path == "/health" ||
		path == "/mcp" ||
		strings.HasPrefix(path, "/mcp/")
}

// originGuard rejects cross-site requests to the API and reflects CORS headers
// for the allowlisted origins only. Requests carrying neither Origin nor
// Sec-Fetch-Site are non-browser clients (the Tauri tray's reqwest calls, the
// dev proxy, curl) and are allowed through: a browser always sends at least one
// of the two, so their joint absence cannot be forged from a page.
func originGuard(port int) func(http.Handler) http.Handler {
	allowed := allowedOrigins(port)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !guarded(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Vary", "Origin")
				if !allowed[origin] {
					writeError(w, http.StatusForbidden, "cross-origin request refused")
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			} else if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
				// Origin is omitted on same-origin GETs, so this catches the
				// cross-site reads that would otherwise slip past the check above.
				w.Header().Add("Vary", "Sec-Fetch-Site")
				writeError(w, http.StatusForbidden, "cross-site request refused")
				return
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
