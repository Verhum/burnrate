package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The `all:` prefix matters twice over: it keeps dotfiles inside a real export,
// and it lets the tracked `out/.gitkeep` satisfy the pattern in a checkout where
// `npm run build` has not run. Without a match here the *compile* fails, taking
// `internal/server` and `cmd/burnrate` down with it — see out/.gitkeep.
//
//go:embed all:out
var content embed.FS

var FS fs.FS

// built records whether the embedded export is a real Next.js build rather than
// just the placeholder that keeps the embed pattern satisfiable.
var built bool

func init() {
	sub, err := fs.Sub(content, "out")
	if err != nil {
		panic(err)
	}
	FS = sub

	if f, err := FS.Open("index.html"); err == nil {
		f.Close()
		built = true
	}
}

// Built reports whether a frontend build is embedded in this binary. It is false
// for a binary built with plain `go build` in a tree where `make web-build` has
// not run — Go code compiles and tests fine there, but there is no UI to serve.
func Built() bool { return built }

// Handler returns an http.Handler that serves the embedded frontend.
// Non-file requests fall back to index.html for SPA client-side routing.
func Handler() http.Handler {
	if !built {
		return http.HandlerFunc(notBuilt)
	}

	fileServer := http.FileServer(http.FS(FS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try serving the file directly; fall back to index.html for SPA routes.
		f, err := FS.Open(path[1:]) // strip leading /
		if err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}

const notBuiltPage = `<!doctype html>
<title>burnrate — frontend not built</title>
<body style="font:14px/1.6 ui-monospace,monospace;max-width:44rem;margin:4rem auto;padding:0 1rem">
<h1 style="font-size:1rem">No frontend embedded in this binary</h1>
<p>The Go server is running, and the JSON API and SSE stream work normally. Only
the static UI is missing: this binary was built without a Next.js export.</p>
<pre>make bootstrap   # once per checkout: web deps
make go-build    # rebuild, embedding the export</pre>
<p><code>make dev</code> skips this entirely — it serves the UI from the Next.js
dev server instead.</p>
</body>
`

// notBuilt explains the missing export instead of 404ing on every path, which is
// what a fresh-checkout binary used to do with no hint as to why.
func notBuilt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(notBuiltPage))
}
