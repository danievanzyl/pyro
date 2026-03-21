package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// SetupUIRoutes serves the embedded SvelteKit static build.
// Falls back to index.html for SPA client-side routing.
func SetupUIRoutes(r chi.Router, uiFS fs.FS) {
	fileServer := http.FileServer(http.FS(uiFS))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Try serving the exact file first.
		if path != "" {
			if f, err := uiFS.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for all non-API, non-file routes.
		// Clone the request to avoid mutating the original.
		indexReq := r.Clone(r.Context())
		indexReq.URL.Path = "/"
		fileServer.ServeHTTP(w, indexReq)
	})
}
