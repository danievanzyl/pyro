package imageops

import (
	"fmt"
	"path/filepath"

	"github.com/danievanzyl/pyro/internal/sandbox/imageconfig"
)

// WriteImageConfig writes the runtime config to {mountDir}/etc/pyro/image-config.json.
// Pure file IO — works on any OS, the mount is just a directory by the time it's called.
func WriteImageConfig(mountDir string, cfg imageconfig.ImageConfig) error {
	target := filepath.Join(mountDir, imageconfig.Path)
	if err := imageconfig.Save(target, &cfg); err != nil {
		return fmt.Errorf("write image config: %w", err)
	}
	return nil
}
