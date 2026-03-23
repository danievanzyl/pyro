package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danievanzyl/pyro/internal/store"
)

type contextKey string

const apiKeyContextKey contextKey = "api_key"

// APIKeyFromContext extracts the authenticated API key from the request context.
func APIKeyFromContext(ctx context.Context) *store.APIKey {
	ak, _ := ctx.Value(apiKeyContextKey).(*store.APIKey)
	return ak
}

// AuthMiddleware validates Bearer token API keys.
func AuthMiddleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			key := strings.TrimPrefix(header, "Bearer ")
			if key == header {
				http.Error(w, `{"error":"invalid Authorization format, expected Bearer <token>"}`, http.StatusUnauthorized)
				return
			}

			ak, err := st.ValidateAPIKey(r.Context(), key)
			if err != nil {
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			if ak == nil {
				http.Error(w, `{"error":"invalid or expired API key"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyContextKey, ak)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
