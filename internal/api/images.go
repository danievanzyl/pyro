package api

import (
	"encoding/json"
	"net/http"

	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/go-chi/chi/v5"
)

// SetupImageRoutes adds image management routes.
func SetupImageRoutes(r chi.Router, imgMgr *sandbox.ImageManager) {
	r.Get("/images", handleListImages(imgMgr))
	r.Get("/images/{name}", handleGetImage(imgMgr))
	r.Post("/images", handleCreateImage(imgMgr))
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
		info, err := imgMgr.Get(name)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

// CreateImageRequest is the body for POST /images.
type CreateImageRequest struct {
	Name       string `json:"name"`
	Dockerfile string `json:"dockerfile"` // path to Dockerfile on the host
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
		if req.Dockerfile == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dockerfile path is required"})
			return
		}

		info, err := imgMgr.CreateFromDockerfile(r.Context(), req.Name, req.Dockerfile)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "build failed: " + err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, info)
	}
}
