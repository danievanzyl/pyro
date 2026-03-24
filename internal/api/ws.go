package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/danievanzyl/pyro/internal/protocol"
	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // API key auth is sufficient
}

// wsExecRequest is sent by the client over WebSocket to execute a command.
type wsExecRequest struct {
	Type    string            `json:"type"` // "exec"
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workdir,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
}

// wsExecOutput is streamed back to the client.
type wsExecOutput struct {
	Type     string `json:"type"` // "stdout", "stderr", "exit", "error"
	Data     string `json:"data,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// SetupWebSocketRoutes adds WebSocket routes to an existing chi router.
func SetupWebSocketRoutes(r chi.Router, mgr *sandbox.Manager, st *store.Store, log *slog.Logger) {
	r.Get("/sandboxes/{id}/ws", handleWebSocketExec(mgr, st, log))
}

func handleWebSocketExec(mgr *sandbox.Manager, st *store.Store, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		// Authenticate via query param (WebSocket can't set headers easily).
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

		// Validate sandbox.
		sb, err := st.GetSandbox(r.Context(), id)
		if err != nil || sb == nil {
			http.Error(w, "sandbox not found", http.StatusNotFound)
			return
		}
		if sb.APIKeyID != ak.ID {
			http.Error(w, "sandbox not found", http.StatusNotFound)
			return
		}
		if sb.IsExpired() {
			http.Error(w, "sandbox has expired", http.StatusGone)
			return
		}
		if sb.State != store.StateRunning {
			http.Error(w, "sandbox is not running", http.StatusConflict)
			return
		}

		// Upgrade to WebSocket.
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("websocket upgrade failed", "err", err)
			return
		}
		defer conn.Close()

		log.Info("websocket connected", "sandbox_id", id)

		// Read commands from WebSocket, execute, stream results back.
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				log.Error("websocket read", "err", err)
				return
			}

			var req wsExecRequest
			if err := json.Unmarshal(msg, &req); err != nil {
				sendWSError(conn, "invalid JSON: "+err.Error(), log)
				continue
			}

			if req.Type != "exec" {
				sendWSError(conn, "unknown message type: "+req.Type, log)
				continue
			}

			if len(req.Command) == 0 {
				sendWSError(conn, "command is required", log)
				continue
			}
			if msg := validateEnvKeys(req.Env); msg != "" {
				sendWSError(conn, msg, log)
				continue
			}

			// Execute command via vsock (sync for now, Phase 2 adds true streaming).
			timeout := 300 * time.Second
			if req.Timeout > 0 {
				timeout = time.Duration(req.Timeout) * time.Second
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)

			protoReq := &protocol.ExecRequest{
				Command: req.Command,
				Env:     req.Env,
				WorkDir: req.WorkDir,
				Timeout: req.Timeout,
			}

			resp, err := mgr.ExecInSandbox(ctx, id, protoReq)
			cancel()

			if err != nil {
				sendWSError(conn, "exec failed: "+err.Error(), log)
				continue
			}

			// Send stdout.
			if resp.Stdout != "" {
				sendWSOutput(conn, "stdout", resp.Stdout, 0, log)
			}
			// Send stderr.
			if resp.Stderr != "" {
				sendWSOutput(conn, "stderr", resp.Stderr, 0, log)
			}
			// Send exit code.
			sendWSOutput(conn, "exit", "", resp.ExitCode, log)
		}
	}
}

func sendWSOutput(conn *websocket.Conn, typ, data string, exitCode int, log *slog.Logger) {
	out := wsExecOutput{Type: typ, Data: data, ExitCode: exitCode}
	msg, _ := json.Marshal(out)
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Error("ws write", "type", typ, "err", err)
	}
}

func sendWSError(conn *websocket.Conn, message string, log *slog.Logger) {
	out := wsExecOutput{Type: "error", Data: message}
	msg, _ := json.Marshal(out)
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Error("ws write error", "err", err)
	}
}
