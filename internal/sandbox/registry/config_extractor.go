package registry

import "github.com/danievanzyl/pyro/internal/sandbox/imageconfig"

// ExtractConfig projects the OCI manifest's Config block into the flat
// runtime config consumed by the in-VM agent. Returns a zero-value config
// when the manifest has no embedded config (e.g. scratch).
func ExtractConfig(m *Manifest) imageconfig.ImageConfig {
	if m == nil || m.Config == nil {
		return imageconfig.ImageConfig{}
	}
	c := m.Config.Config
	out := imageconfig.ImageConfig{
		WorkDir: c.WorkingDir,
		User:    c.User,
	}
	if len(c.Env) > 0 {
		out.Env = append([]string(nil), c.Env...)
	}
	return out
}
