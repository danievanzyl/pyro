// Package imageops builds rootfs ext4 filesystems from OCI image layers.
//
// LayerExtractor is pure Go and runnable on any OS — applies an OCI layer's
// tar stream onto a target directory with whiteout and opaque-dir handling.
// Ext4Builder and AgentInjector are Linux-only (loop mount + mkfs.ext4).
package imageops

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	whiteoutPrefix = ".wh."
	opaqueMarker   = ".wh..wh..opq"
)

// LayerExtractor applies image layers to a target directory.
type LayerExtractor struct{}

// NewLayerExtractor constructs an extractor.
func NewLayerExtractor() *LayerExtractor {
	return &LayerExtractor{}
}

// Extract reads a tar stream (uncompressed OCI/Docker layer) and applies it
// onto rootDir. Whiteout entries (.wh.<name>) cause the named sibling to be
// removed; opaque markers (.wh..wh..opq) clear the parent directory before
// applying further entries from this layer.
func (e *LayerExtractor) Extract(rootDir string, layer io.Reader) error {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return fmt.Errorf("ensure root: %w", err)
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("abs root: %w", err)
	}

	tr := tar.NewReader(layer)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if err := e.applyEntry(absRoot, hdr, tr); err != nil {
			return fmt.Errorf("apply %q: %w", hdr.Name, err)
		}
	}
}

func (e *LayerExtractor) applyEntry(root string, hdr *tar.Header, body io.Reader) error {
	cleaned := filepath.Clean("/" + hdr.Name)
	target := filepath.Join(root, cleaned)
	// Reject path escapes via tar contents.
	if !strings.HasPrefix(target+string(filepath.Separator), root+string(filepath.Separator)) && target != root {
		return fmt.Errorf("path escape: %s", hdr.Name)
	}

	base := filepath.Base(target)
	parent := filepath.Dir(target)

	switch {
	case base == opaqueMarker:
		// Clear contents of parent dir but keep the directory itself.
		return clearDir(parent)
	case strings.HasPrefix(base, whiteoutPrefix):
		victim := filepath.Join(parent, strings.TrimPrefix(base, whiteoutPrefix))
		return os.RemoveAll(victim)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, fileMode(hdr.Mode, 0o755)); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("mkdir parent: %w", err)
		}
		// Replace any pre-existing entry to avoid permission/symlink surprises.
		_ = os.RemoveAll(target)
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode(hdr.Mode, 0o644))
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		if _, err := io.Copy(f, body); err != nil {
			f.Close()
			return fmt.Errorf("copy: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}
	case tar.TypeSymlink:
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("mkdir parent: %w", err)
		}
		_ = os.RemoveAll(target)
		if err := os.Symlink(hdr.Linkname, target); err != nil {
			return fmt.Errorf("symlink: %w", err)
		}
	case tar.TypeLink:
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("mkdir parent: %w", err)
		}
		_ = os.RemoveAll(target)
		linkTarget := filepath.Join(root, filepath.Clean("/"+hdr.Linkname))
		if err := os.Link(linkTarget, target); err != nil {
			return fmt.Errorf("hardlink: %w", err)
		}
	default:
		// Skip char/block/fifo — Firecracker rootfs doesn't need them at this layer.
	}
	return nil
}

// clearDir removes everything inside dir but leaves dir itself in place.
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if err := os.RemoveAll(filepath.Join(dir, ent.Name())); err != nil {
			return err
		}
	}
	return nil
}

func fileMode(m int64, fallback os.FileMode) os.FileMode {
	if m == 0 {
		return fallback
	}
	return os.FileMode(m & 0o7777)
}
