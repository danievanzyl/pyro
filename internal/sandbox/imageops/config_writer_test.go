package imageops

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danievanzyl/pyro/internal/sandbox/imageconfig"
)

func TestWriteImageConfig_RoundTrip(t *testing.T) {
	root := t.TempDir()
	cfg := imageconfig.ImageConfig{
		Env:     []string{"PATH=/usr/local/bin:/usr/bin", "PYTHONUNBUFFERED=1"},
		WorkDir: "/app",
		User:    "1000",
	}
	if err := WriteImageConfig(root, cfg); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Verify file landed at the canonical relative path.
	got, err := imageconfig.Load(filepath.Join(root, imageconfig.Path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(*got, cfg) {
		t.Errorf("roundtrip mismatch\n got=%+v\nwant=%+v", *got, cfg)
	}
}

func TestWriteImageConfig_CreatesParentDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rootfs")
	cfg := imageconfig.ImageConfig{User: "0"}
	if err := WriteImageConfig(root, cfg); err != nil {
		t.Fatalf("write: %v", err)
	}
}
