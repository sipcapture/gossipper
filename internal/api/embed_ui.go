package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// webControlUIDist holds the production build of web/control-ui (Vite outDir → internal/api/webdist).
// Run `make frontend` before `go build` if this directory is empty (only .gitkeep).
//
//go:embed all:webdist
var webControlUIDist embed.FS

// HasEmbeddedControlUI reports whether a real UI build is present (index.html from Vite).
func HasEmbeddedControlUI() bool {
	_, err := webControlUIDist.ReadFile("webdist/index.html")
	return err == nil
}

func registerEmbeddedControlUI(mux *http.ServeMux) {
	if !HasEmbeddedControlUI() {
		return
	}
	registerEmbeddedControlUIOnMux(mux)
}

// RegisterEmbeddedControlUI mounts GET / and /assets/* when a Vite build is
// embedded (internal/api/webdist). No-op when only .gitkeep is present.
func RegisterEmbeddedControlUI(mux *http.ServeMux) {
	registerEmbeddedControlUI(mux)
}

func registerEmbeddedControlUIOnMux(mux *http.ServeMux) {
	root, err := fs.Sub(webControlUIDist, "webdist")
	if err != nil {
		return
	}
	assets, err := fs.Sub(root, "assets")
	if err != nil {
		return
	}
	mux.Handle(
		"/assets/",
		http.StripPrefix("/assets/", http.FileServer(http.FS(assets))),
	)
	serveIndex := func(w http.ResponseWriter, _ *http.Request) {
		data, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	}
	mux.HandleFunc("GET /{$}", serveIndex)
	mux.HandleFunc("HEAD /{$}", serveIndex)
}
