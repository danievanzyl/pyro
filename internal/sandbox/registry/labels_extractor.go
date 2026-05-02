package registry

// ExtractLabels returns a copy of the OCI manifest's Config.Labels map.
// Returns nil when the manifest, its config, or the labels map is empty.
// Labels are host-side metadata (provenance, build info) — distinct from
// ImageConfig which is consumed by the in-VM agent.
func ExtractLabels(m *Manifest) map[string]string {
	if m == nil || m.Config == nil {
		return nil
	}
	src := m.Config.Config.Labels
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
