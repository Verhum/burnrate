package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The embedded content legitimately differs between a tree where `make web-build`
// has run and one where it has not — that is the whole point of the tracked
// out/.gitkeep. Both states must serve something intelligible rather than a bare
// 404, so assert against whichever state this tree is in.

func TestHandlerServesRootInEitherState(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if Built() {
		if rec.Code != http.StatusOK {
			t.Fatalf("built frontend: GET / = %d, want 200", rec.Code)
		}
		return
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unbuilt frontend: GET / = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "make bootstrap") {
		t.Errorf("unbuilt frontend: response should name the fix, got:\n%s", body)
	}
}

// A missing export used to make every path 404 with no hint as to why. An
// unbuilt binary should say so on SPA routes too, not just at the root.
func TestHandlerExplainsMissingExportOnAnyPath(t *testing.T) {
	if Built() {
		t.Skip("frontend is built in this tree")
	}

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /settings = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// Guards the reason out/.gitkeep is tracked: this package must compile and its
// FS must be usable without any frontend build having run. If the placeholder is
// ever untracked again, the package stops compiling and this test cannot even
// run — which is the loud failure we want, versus a silently empty embed.
func TestEmbeddedFSIsUsableWithoutABuild(t *testing.T) {
	if FS == nil {
		t.Fatal("FS is nil after init")
	}
	if _, err := FS.Open("."); err != nil {
		t.Fatalf("embedded FS root not openable: %v", err)
	}
}
