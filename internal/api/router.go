package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danievanzyl/pyro/internal/observability"
	"github.com/danievanzyl/pyro/internal/protocol"
	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServerConfig holds optional dependencies for the API server.
type ServerConfig struct {
	Metrics  *observability.Metrics
	Quota    *QuotaEnforcer
	ImageMgr *sandbox.ImageManager
	UIFS     fs.FS // embedded SvelteKit build
	EventBus *EventBus
	Pool     *sandbox.Pool
}

// Server is the HTTP API server.
type Server struct {
	manager  *sandbox.Manager
	store    *store.Store
	log      *slog.Logger
	router   chi.Router
	metrics  *observability.Metrics
	quota    *QuotaEnforcer
	imageMgr *sandbox.ImageManager
	uiFS     fs.FS
	eventBus *EventBus
	pool     *sandbox.Pool
}

// NewServer creates an API server.
func NewServer(manager *sandbox.Manager, st *store.Store, log *slog.Logger, cfg *ServerConfig) *Server {
	s := &Server{
		manager: manager,
		store:   st,
		log:     log,
	}
	if cfg != nil {
		s.metrics = cfg.Metrics
		s.quota = cfg.Quota
		s.imageMgr = cfg.ImageMgr
		s.uiFS = cfg.UIFS
		s.eventBus = cfg.EventBus
		s.pool = cfg.Pool
	}
	s.setupRoutes()
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	if s.metrics != nil {
		r.Use(MetricsMiddleware(s.metrics))
	}

	// All API routes under /api/ prefix.
	r.Route("/api", func(r chi.Router) {
		// Health check (unauthenticated).
		r.Get("/health", s.handleHealth)

		// Authenticated routes.
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(s.store))

			r.Post("/sandboxes", s.handleCreateSandbox)
			r.Get("/sandboxes", s.handleListSandboxes)
			r.Get("/sandboxes/{id}", s.handleGetSandbox)
			r.Delete("/sandboxes/{id}", s.handleDeleteSandbox)
			r.Post("/sandboxes/{id}/exec", s.handleExecInSandbox)
			r.Put("/sandboxes/{id}/files/*", s.handleFileWrite)
			r.Get("/sandboxes/{id}/files/*", s.handleFileRead)
		})

		// Authenticated image + audit routes.
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(s.store))
			if s.imageMgr != nil {
				SetupImageRoutes(r, s.imageMgr)
			}
			SetupAuditRoutes(r, s.store)
		})

		// WebSocket routes (auth via query param, not middleware).
		SetupWebSocketRoutes(r, s.manager, s.store, s.log)

		// SSE event stream (auth via query param).
		if s.eventBus != nil {
			SetupSSERoutes(r, s.eventBus, s.store, s.log)
		}
	})

	// Backward compat: /health without /api prefix.
	r.Get("/health", s.handleHealth)

	// Prometheus metrics endpoint.
	if s.metrics != nil {
		r.Handle("/metrics", promhttp.Handler())
	}

	// Embedded UI (must be last — catch-all for SPA routing).
	if s.uiFS != nil {
		SetupUIRoutes(r, s.uiFS)
	}

	s.router = r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":           "ok",
		"active_sandboxes": s.manager.ActiveCount(),
	}
	if s.pool != nil {
		resp["pool_stats"] = s.pool.Stats()
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateSandboxRequest is the request body for POST /sandboxes.
type CreateSandboxRequest struct {
	TTL    int    `json:"ttl"`              // seconds
	Image  string `json:"image"`            // rootfs image name (default: "default")
	VCPU   int    `json:"vcpu,omitempty"`   // vCPU count (0 = image/server default)
	MemMiB int    `json:"mem_mib,omitempty"` // memory in MiB (0 = image/server default)
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	var req CreateSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	if req.TTL <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ttl must be > 0 (seconds)"})
		return
	}
	if req.TTL > 86400 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ttl must be <= 86400 (24 hours)"})
		return
	}

	image := req.Image
	if image == "" {
		image = "default"
	}

	ak := APIKeyFromContext(r.Context())
	ttl := time.Duration(req.TTL) * time.Second

	// Quota check.
	if s.quota != nil {
		if err := s.quota.CheckCreateQuota(r.Context(), ak.ID, req.TTL); err != nil {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
	}

	start := time.Now()
	sb, err := s.manager.CreateSandbox(r.Context(), ak.ID, image, ttl, sandbox.VMResources{
		VCPU:   req.VCPU,
		MemMiB: req.MemMiB,
	})
	if err != nil {
		s.log.Error("create sandbox failed", "err", err)
		if s.metrics != nil {
			reason := "internal"
			if isCapacityError(err) {
				reason = "capacity"
			}
			s.metrics.RecordCreateFailed(r.Context(), image, reason)
		}
		if isCapacityError(err) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create sandbox"})
		return
	}

	// Record metrics.
	if s.metrics != nil {
		s.metrics.RecordSandboxCreated(r.Context(), image, time.Since(start))
	}

	// Audit log + SSE event.
	s.store.LogAudit(r.Context(), &store.AuditEntry{
		Action:    store.AuditSandboxCreated,
		APIKeyID:  ak.ID,
		SandboxID: sb.ID,
		Detail:    fmt.Sprintf("image=%s ttl=%ds", image, req.TTL),
	})
	s.publishEvent("sandbox.created", sb)

	writeJSON(w, http.StatusCreated, sb)
}

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	ak := APIKeyFromContext(r.Context())
	sandboxes, err := s.store.ListSandboxes(r.Context(), ak.ID)
	if err != nil {
		s.log.Error("list sandboxes failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sandboxes"})
		return
	}
	if sandboxes == nil {
		sandboxes = []*store.Sandbox{}
	}
	writeJSON(w, http.StatusOK, sandboxes)
}

func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sb, err := s.store.GetSandbox(r.Context(), id)
	if err != nil {
		s.log.Error("get sandbox failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if sb == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}

	// Check ownership.
	ak := APIKeyFromContext(r.Context())
	if sb.APIKeyID != ak.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}

	writeJSON(w, http.StatusOK, sb)
}

func (s *Server) handleDeleteSandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check existence and ownership.
	sb, err := s.store.GetSandbox(r.Context(), id)
	if err != nil {
		s.log.Error("get sandbox failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if sb == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}

	ak := APIKeyFromContext(r.Context())
	if sb.APIKeyID != ak.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}

	if err := s.manager.DestroySandbox(r.Context(), id); err != nil {
		s.log.Error("destroy sandbox failed", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to destroy sandbox"})
		return
	}

	if s.metrics != nil {
		s.metrics.RecordSandboxDestroyed(r.Context(), "manual", sb.RemainingTTL())
	}
	s.store.LogAudit(r.Context(), &store.AuditEntry{
		Action: store.AuditSandboxDestroyed, APIKeyID: ak.ID, SandboxID: id, Detail: "reason=manual",
	})
	s.publishEvent("sandbox.destroyed", map[string]string{"id": id, "reason": "manual"})

	w.WriteHeader(http.StatusNoContent)
}

// ExecRequest is the request body for POST /sandboxes/:id/exec.
type ExecRequest struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workdir,omitempty"`
	Timeout int               `json:"timeout,omitempty"` // seconds
}

func (s *Server) handleExecInSandbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check ownership.
	sb, err := s.store.GetSandbox(r.Context(), id)
	if err != nil {
		s.log.Error("get sandbox failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if sb == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}
	ak := APIKeyFromContext(r.Context())
	if sb.APIKeyID != ak.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return
	}
	if sb.IsExpired() {
		writeJSON(w, http.StatusGone, map[string]string{"error": "sandbox has expired"})
		return
	}
	if sb.State != store.StateRunning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "sandbox is not running"})
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(req.Command) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}

	protoReq := &protocol.ExecRequest{
		Command: req.Command,
		Env:     req.Env,
		WorkDir: req.WorkDir,
		Timeout: req.Timeout,
	}

	start := time.Now()
	resp, err := s.manager.ExecInSandbox(r.Context(), id, protoReq)
	if err != nil {
		s.log.Error("exec failed", "id", id, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "exec failed: " + err.Error()})
		return
	}

	if s.metrics != nil {
		s.metrics.RecordExec(r.Context(), time.Since(start), resp.ExitCode)
	}
	s.store.LogAudit(r.Context(), &store.AuditEntry{
		Action: store.AuditSandboxExec, APIKeyID: ak.ID, SandboxID: id,
		Detail: fmt.Sprintf("cmd=%s exit=%d", strings.Join(req.Command, " "), resp.ExitCode),
	})
	s.publishEvent("sandbox.exec", map[string]any{"sandbox_id": id, "exit_code": resp.ExitCode})

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	filePath := "/" + chi.URLParam(r, "*")

	if ok := s.requireRunningSandbox(w, r, id); !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024)) // 50MB max
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	contentType := r.Header.Get("Content-Type")
	binary := contentType != "" && !strings.HasPrefix(contentType, "text/")

	var content string
	if binary {
		content = base64.StdEncoding.EncodeToString(body)
	} else {
		content = string(body)
	}

	protoReq := &protocol.FileWriteRequest{
		Path:    filePath,
		Content: content,
		Mode:    0644,
		Binary:  binary,
	}

	resp, err := s.manager.WriteFileInSandbox(r.Context(), id, protoReq)
	if err != nil {
		s.log.Error("file write failed", "id", id, "path", filePath, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "file write failed: " + err.Error()})
		return
	}

	ak := APIKeyFromContext(r.Context())
	s.store.LogAudit(r.Context(), &store.AuditEntry{
		Action: store.AuditSandboxFileWrite, APIKeyID: ak.ID, SandboxID: id,
		Detail: fmt.Sprintf("path=%s bytes=%d", filePath, resp.BytesWritten),
	})

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	filePath := "/" + chi.URLParam(r, "*")

	if ok := s.requireRunningSandbox(w, r, id); !ok {
		return
	}

	protoReq := &protocol.FileReadRequest{Path: filePath}
	resp, err := s.manager.ReadFileFromSandbox(r.Context(), id, protoReq)
	if err != nil {
		s.log.Error("file read failed", "id", id, "path", filePath, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "file read failed: " + err.Error()})
		return
	}

	data, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "decode content"})
		return
	}

	ak := APIKeyFromContext(r.Context())
	s.store.LogAudit(r.Context(), &store.AuditEntry{
		Action: store.AuditSandboxFileRead, APIKeyID: ak.ID, SandboxID: id,
		Detail: fmt.Sprintf("path=%s size=%d", filePath, resp.Size),
	})

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-File-Mode", fmt.Sprintf("%o", resp.Mode))
	w.Header().Set("X-File-Size", fmt.Sprintf("%d", resp.Size))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// requireRunningSandbox validates ownership and state, writes error response if invalid.
func (s *Server) requireRunningSandbox(w http.ResponseWriter, r *http.Request, id string) bool {
	sb, err := s.store.GetSandbox(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return false
	}
	if sb == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return false
	}
	ak := APIKeyFromContext(r.Context())
	if sb.APIKeyID != ak.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox not found"})
		return false
	}
	if sb.IsExpired() {
		writeJSON(w, http.StatusGone, map[string]string{"error": "sandbox has expired"})
		return false
	}
	if sb.State != store.StateRunning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "sandbox is not running"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ErrAtCapacity is returned when the sandbox manager can't create more VMs.
var ErrAtCapacity = fmt.Errorf("at capacity")

func isCapacityError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "at capacity")
}

func (s *Server) publishEvent(eventType string, data any) {
	if s.eventBus != nil {
		s.eventBus.Publish(eventType, data)
	}
}
