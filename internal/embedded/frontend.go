package embedded

import (
	"io/fs"
	"net/http"
	"strings"
)

// frontendFS is populated at init time from cmd/klaudio via RegisterFrontend.
var frontendFS fs.FS

// RegisterFrontend stores the embedded frontend filesystem.
// Called once at startup from cmd/klaudio where the go:embed directive lives.
func RegisterFrontend(fsys fs.FS) {
	frontendFS = fsys
}

// HasFrontend returns true if the frontend was embedded at build time.
func HasFrontend() bool {
	if frontendFS == nil {
		return false
	}
	entries, err := fs.ReadDir(frontendFS, ".")
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// FrontendHandler returns an http.Handler that serves the embedded frontend.
// For SPA routing, any path that doesn't match a file falls back to index.html.
func FrontendHandler() http.Handler {
	if frontendFS == nil {
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(frontendFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the file exists in the embedded FS
		if f, err := frontendFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for any unmatched route
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
