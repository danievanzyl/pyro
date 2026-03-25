package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/danievanzyl/pyro/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// SetupKeyRoutes adds API key management routes.
func SetupKeyRoutes(r chi.Router, st *store.Store) {
	r.Get("/keys", handleListKeys(st))
	r.Post("/keys", handleCreateKey(st))
	r.Delete("/keys/{id}", handleDeleteKey(st))
}

// keyResponse is the JSON response for an API key (key field masked for list).
type keyResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

func maskKey(key string) string {
	if len(key) > 7 {
		return key[:7] + "..."
	}
	return "***"
}

func handleListKeys(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := st.ListAPIKeys(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list keys: " + err.Error()})
			return
		}
		resp := make([]keyResponse, 0, len(keys))
		for _, k := range keys {
			resp = append(resp, keyResponse{
				ID:        k.ID,
				Name:      k.Name,
				Prefix:    maskKey(k.Key),
				CreatedAt: k.CreatedAt,
				ExpiresAt: k.ExpiresAt,
			})
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// CreateKeyRequest is the body for POST /keys.
type CreateKeyRequest struct {
	Name string `json:"name"`
}

func handleCreateKey(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateKeyRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Name == "" {
			req.Name = "default"
		}
		if !validName.MatchString(req.Name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key name"})
			return
		}

		key := generateKey()
		ak := &store.APIKey{
			ID:        uuid.New().String(),
			Key:       key,
			Name:      req.Name,
			CreatedAt: time.Now().UTC(),
		}

		if err := st.CreateAPIKey(r.Context(), ak); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create key: " + err.Error()})
			return
		}

		// Return the full key only on creation — it can't be retrieved later.
		writeJSON(w, http.StatusCreated, map[string]string{
			"id":   ak.ID,
			"name": ak.Name,
			"key":  ak.Key,
		})
	}
}

func handleDeleteKey(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := st.DeleteAPIKey(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete key: " + err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func generateKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "pk_" + hex.EncodeToString(b)
}
