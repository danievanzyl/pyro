package api

import (
	"net/http"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/observability"
	"github.com/go-chi/chi/v5"
)

// MetricsMiddleware records HTTP request metrics via OTEL.
func MetricsMiddleware(m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(ww, r)

			// Use chi route pattern for path label (avoids high cardinality from IDs).
			routePattern := chi.RouteContext(r.Context()).RoutePattern()
			if routePattern == "" {
				routePattern = r.URL.Path
			}

			m.RecordAPIRequest(r.Context(), r.Method, routePattern, ww.statusCode, time.Since(start))
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
