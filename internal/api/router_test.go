package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// Create a test API key.
	ak := &store.APIKey{
		ID:        "test-key-id",
		Key:       "fclk_testkey",
		Name:      "test",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateAPIKey(context.Background(), ak); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHealthEndpoint(t *testing.T) {
	st := setupTestStore(t)
	// Server needs a manager, but health endpoint doesn't use it heavily.
	// For now, test the store-level parts and auth middleware.
	_ = st

	// Test auth middleware directly.
	t.Run("missing auth header", func(t *testing.T) {
		handler := AuthMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/sandboxes", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid auth format", func(t *testing.T) {
		handler := AuthMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/sandboxes", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		handler := AuthMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/sandboxes", nil)
		req.Header.Set("Authorization", "Bearer fclk_wrongkey")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid key", func(t *testing.T) {
		var gotKey *store.APIKey
		handler := AuthMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = APIKeyFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/sandboxes", nil)
		req.Header.Set("Authorization", "Bearer fclk_testkey")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if gotKey == nil {
			t.Fatal("expected API key in context")
		}
		if gotKey.ID != "test-key-id" {
			t.Errorf("key ID = %q, want %q", gotKey.ID, "test-key-id")
		}
	})
}

func TestCreateSandboxValidation(t *testing.T) {
	// Test request validation (doesn't need a real manager).
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"empty body", `{}`, http.StatusBadRequest},
		{"missing ttl", `{"image":"default"}`, http.StatusBadRequest},
		{"negative ttl", `{"ttl":-1}`, http.StatusBadRequest},
		{"ttl too large", `{"ttl":100000}`, http.StatusBadRequest},
		{"invalid json", `{bad`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily test the full handler without a manager,
			// but we can verify the validation logic by checking the response.
			req := httptest.NewRequest("POST", "/sandboxes", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			// Simulate authenticated context.
			ctx := context.WithValue(req.Context(), apiKeyContextKey, &store.APIKey{ID: "test"})
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			// Inline the validation part of handleCreateSandbox.
			s := &Server{}
			s.handleCreateSandbox(w, req)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.status, w.Body.String())
			}
		})
	}
}
