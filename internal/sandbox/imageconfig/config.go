// Package imageconfig defines the OCI image runtime config shared between
// host-side image registration (registry pull / Dockerfile build) and the
// in-VM agent's exec handler. Plain types + pure logic — runs on any OS.
package imageconfig

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Path is the canonical location inside a sandbox rootfs where the image's
// runtime defaults are stored. Written at registration time; read by the
// agent at boot.
const Path = "/etc/pyro/image-config.json"

// ImageConfig holds the runtime defaults extracted from an OCI image's
// Config block (or a Dockerfile build's docker-inspect output).
type ImageConfig struct {
	Env     []string `json:"env,omitempty"`
	WorkDir string   `json:"workdir,omitempty"`
	User    string   `json:"user,omitempty"`
}

// Load reads an ImageConfig from path. Returns a zero-value config when the
// file does not exist (preserves backward-compat for pre-existing images).
func Load(path string) (*ImageConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &ImageConfig{}, nil
		}
		return nil, err
	}
	cfg := &ImageConfig{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes cfg as JSON to path, creating the parent directory at 0o755.
func Save(path string, cfg *ImageConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// MergeEnv overlays the per-request env map onto the image's KEY=VAL list.
// Per-request entries override the image defaults on key collision; image
// vars not mentioned in req survive. Returns a flat KEY=VAL slice usable
// directly as exec.Cmd.Env.
func MergeEnv(image []string, req map[string]string) []string {
	out := make([]string, 0, len(image)+len(req))
	seen := make(map[string]int, len(image))
	for _, kv := range image {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if v, override := req[k]; override {
			seen[k] = len(out)
			out = append(out, k+"="+v)
			continue
		}
		seen[k] = len(out)
		out = append(out, kv)
	}
	for k, v := range req {
		if _, ok := seen[k]; ok {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}

// ResolveCwd returns reqCwd if set, otherwise the image's WorkDir.
func ResolveCwd(image *ImageConfig, reqCwd string) string {
	if reqCwd != "" {
		return reqCwd
	}
	if image == nil {
		return ""
	}
	return image.WorkDir
}

// PasswdLookup resolves a username to a numeric UID, returning ok=false if
// the name is not present. Pluggable so tests don't depend on /etc/passwd.
type PasswdLookup func(name string) (uid int, ok bool)

// ResolveUID interprets the image's USER directive.
//   - empty user → ok=false (caller runs as default, typically root).
//   - numeric UID (or "uid:gid") → ok=true with that UID.
//   - name that resolves via lookup → ok=true with the resolved UID.
//   - name that does not resolve → fellBack=true, ok=false (caller runs as root).
func ResolveUID(user string, lookup PasswdLookup) (uid int, ok bool, fellBack bool) {
	if user == "" {
		return 0, false, false
	}
	u, _, _ := strings.Cut(user, ":")
	if u == "" {
		return 0, false, false
	}
	if n, err := strconv.Atoi(u); err == nil {
		return n, true, false
	}
	if lookup != nil {
		if id, found := lookup(u); found {
			return id, true, false
		}
	}
	return 0, false, true
}
