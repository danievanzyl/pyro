package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/sandbox/imageops"
	"github.com/danievanzyl/pyro/internal/sandbox/imagestate"
	"github.com/go-chi/chi/v5"
)

// SetupImageRoutes adds image management routes.
func SetupImageRoutes(r chi.Router, imgMgr *sandbox.ImageManager) {
	r.Get("/images", handleListImages(imgMgr))
	r.Get("/images/{name}", handleGetImage(imgMgr))
	r.Post("/images", handleCreateImage(imgMgr))
	r.Get("/kernels", handleListKernels(imgMgr))
}

func handleListKernels(imgMgr *sandbox.ImageManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kernels, err := imgMgr.ListKernels()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if kernels == nil {
			kernels = []*sandbox.KernelInfo{}
		}
		writeJSON(w, http.StatusOK, kernels)
	}
}

func handleListImages(imgMgr *sandbox.ImageManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		images, err := imgMgr.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if images == nil {
			images = []*sandbox.ImageInfo{}
		}
		writeJSON(w, http.StatusOK, images)
	}
}

func handleGetImage(imgMgr *sandbox.ImageManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if !validName.MatchString(name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid image name"})
			return
		}
		// Status() prefers ledger over disk so in-flight and recently-failed
		// pulls are visible.
		info := imgMgr.Status(name)
		if info == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

// CreateImageRequest is the body for POST /images.
//
// Exactly one of Dockerfile or Source must be set.
type CreateImageRequest struct {
	Name       string `json:"name"`
	Dockerfile string `json:"dockerfile,omitempty"` // path to Dockerfile on the host
	Source     string `json:"source,omitempty"`     // OCI image reference, e.g. "python:3.12"
}

func handleCreateImage(imgMgr *sandbox.ImageManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if !validName.MatchString(req.Name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid image name"})
			return
		}
		hasDockerfile := req.Dockerfile != ""
		hasSource := req.Source != ""
		if hasDockerfile == hasSource {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "exactly one of dockerfile or source must be set",
			})
			return
		}

		if hasSource {
			// Async: returns immediately with status=pulling.
			// ErrSourceConflict → 409 (concurrent caller with a different
			// source for the same name). Anything else → 500.
			info, err := imgMgr.CreateFromRegistry(r.Context(), req.Name, req.Source, nil)
			if err != nil {
				if errors.Is(err, imagestate.ErrSourceConflict) {
					writeJSON(w, http.StatusConflict, map[string]string{
						"error": err.Error() + " — wait for completion or use force",
					})
					return
				}
				var tooLarge *imageops.ImageTooLargeError
				if errors.As(err, &tooLarge) {
					writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
						"error":        "image too large",
						"limit_mb":     tooLarge.LimitMB,
						"estimated_mb": tooLarge.EstimatedMB,
					})
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "register failed: " + err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, info)
			return
		}

		// Dockerfile path stays synchronous in this slice.
		info, err := imgMgr.CreateFromDockerfile(r.Context(), req.Name, req.Dockerfile)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "build failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, info)
	}
}
