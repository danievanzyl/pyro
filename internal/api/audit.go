package api

import (
	"net/http"
	"strconv"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
	"github.com/go-chi/chi/v5"
)

// SetupAuditRoutes adds audit log routes.
func SetupAuditRoutes(r chi.Router, st *store.Store) {
	r.Get("/audit", handleListAudit(st))
	r.Get("/audit/sandbox/{id}", handleSandboxAudit(st))
}

func handleListAudit(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		entries, err := st.ListAuditEntries(r.Context(), limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []*store.AuditEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

func handleSandboxAudit(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		entries, err := st.ListAuditBySandbox(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []*store.AuditEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}
