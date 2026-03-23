package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danievanzyl/pyro/internal/store"
	"github.com/go-chi/chi/v5"
)

// SetupSSERoutes adds the Server-Sent Events endpoint.
func SetupSSERoutes(r chi.Router, bus *EventBus, st *store.Store, log *slog.Logger) {
	r.Get("/events", handleSSE(bus, st, log))
}

func handleSSE(bus *EventBus, st *store.Store, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth via query param (like WebSocket — SSE can't set headers).
		apiKey := r.URL.Query().Get("api_key")
		if apiKey == "" {
			http.Error(w, "missing api_key query parameter", http.StatusUnauthorized)
			return
		}
		ak, err := st.ValidateAPIKey(r.Context(), apiKey)
		if err != nil || ak == nil {
			http.Error(w, "invalid or expired API key", http.StatusUnauthorized)
			return
		}

		// Set SSE headers.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Subscribe to event bus.
		ch := bus.Subscribe()
		defer bus.Unsubscribe(ch)

		log.Info("sse client connected", "api_key", ak.Name, "clients", bus.ClientCount())

		// Send initial connection event.
		writeSSE(w, flusher, "connected", map[string]string{"status": "ok"})

		// Stream events until client disconnects.
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				log.Info("sse client disconnected", "api_key", ak.Name)
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				writeSSE(w, flusher, event.Type, event)
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	flusher.Flush()
}
