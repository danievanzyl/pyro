// Package registry pulls OCI/Docker images from a remote registry without a
// Docker daemon. Wraps go-containerregistry. Pinned to linux/amd64.
package registry

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	platformOS   = "linux"
	platformArch = "amd64"
)

// ErrNoAmd64Variant is returned when a manifest index has no linux/amd64 variant.
var ErrNoAmd64Variant = errors.New("manifest has no linux/amd64 variant")

// LayerInfo summarizes a single image layer.
type LayerInfo struct {
	Digest    string
	Size      int64
	MediaType string
}

// Manifest is the resolved single-arch image manifest.
type Manifest struct {
	Digest string
	Layers []LayerInfo
	Config *v1.ConfigFile
	image  v1.Image
}

// Puller pulls images from a remote registry.
type Puller struct {
	keychain authn.Keychain
	// transport overrides remote transport; tests inject httptest servers.
	options []remote.Option
}

// New constructs a Puller using DefaultKeychain (reads ~/.docker/config.json
// and standard credential helpers).
func New() *Puller {
	return &Puller{keychain: authn.DefaultKeychain}
}

// WithOptions returns a Puller with extra remote options applied.
// Used in tests to point at httptest registries.
func (p *Puller) WithOptions(opts ...remote.Option) *Puller {
	cp := *p
	cp.options = append(append([]remote.Option{}, p.options...), opts...)
	return &cp
}

func (p *Puller) baseOpts(ctx context.Context) []remote.Option {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(p.keychain),
	}
	return append(opts, p.options...)
}

// Resolve fetches the manifest for ref, selecting the linux/amd64 variant if
// the registry returns an index. Returns ErrNoAmd64Variant if no amd64 variant exists.
func (p *Puller) Resolve(ctx context.Context, ref string) (*Manifest, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parse reference: %w", err)
	}

	desc, err := remote.Get(parsed, p.baseOpts(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("fetch descriptor: %w", err)
	}

	img, err := p.imageForDescriptor(ctx, parsed, desc)
	if err != nil {
		return nil, err
	}

	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("image digest: %w", err)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("image config: %w", err)
	}

	rawLayers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("image layers: %w", err)
	}

	layers := make([]LayerInfo, 0, len(rawLayers))
	for _, l := range rawLayers {
		d, err := l.Digest()
		if err != nil {
			return nil, fmt.Errorf("layer digest: %w", err)
		}
		size, err := l.Size()
		if err != nil {
			return nil, fmt.Errorf("layer size: %w", err)
		}
		mt, err := l.MediaType()
		if err != nil {
			return nil, fmt.Errorf("layer media type: %w", err)
		}
		layers = append(layers, LayerInfo{
			Digest:    d.String(),
			Size:      size,
			MediaType: string(mt),
		})
	}

	return &Manifest{
		Digest: digest.String(),
		Layers: layers,
		Config: cfg,
		image:  img,
	}, nil
}

// LayerSizes resolves the manifest for ref and returns each layer's
// compressed size in bytes. Used by the size-cap check before any layer
// bytes are downloaded.
//
// Equivalent cost to Resolve — the registry returns layer sizes inside
// the manifest itself, no separate per-layer HEAD needed.
func (p *Puller) LayerSizes(ctx context.Context, ref string) ([]int64, error) {
	m, err := p.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	sizes := make([]int64, 0, len(m.Layers))
	for _, l := range m.Layers {
		sizes = append(sizes, l.Size)
	}
	return sizes, nil
}

// imageForDescriptor unwraps an index to its linux/amd64 child, or returns the
// single-arch image directly.
func (p *Puller) imageForDescriptor(ctx context.Context, ref name.Reference, desc *remote.Descriptor) (v1.Image, error) {
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("parse index: %w", err)
		}
		manifest, err := idx.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("read index manifest: %w", err)
		}
		for _, m := range manifest.Manifests {
			if m.Platform != nil && m.Platform.OS == platformOS && m.Platform.Architecture == platformArch {
				child, err := idx.Image(m.Digest)
				if err != nil {
					return nil, fmt.Errorf("fetch amd64 child: %w", err)
				}
				return child, nil
			}
		}
		return nil, ErrNoAmd64Variant
	default:
		img, err := desc.Image()
		if err != nil {
			return nil, fmt.Errorf("parse image: %w", err)
		}
		// For single-arch images, verify the embedded config matches amd64.
		cfg, err := img.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if cfg.OS != "" && cfg.OS != platformOS {
			return nil, ErrNoAmd64Variant
		}
		if cfg.Architecture != "" && cfg.Architecture != platformArch {
			return nil, ErrNoAmd64Variant
		}
		return img, nil
	}
}

// LayerReader opens a layer's uncompressed tar stream by digest.
// The caller must close the reader.
func (m *Manifest) LayerReader(digest string) (io.ReadCloser, error) {
	for _, l := range m.layersByDigest() {
		d, err := l.Digest()
		if err != nil {
			return nil, err
		}
		if d.String() == digest {
			return l.Uncompressed()
		}
	}
	return nil, fmt.Errorf("layer %s not found in manifest", digest)
}

// CompressedLayerReader opens the on-the-wire (compressed) byte stream
// for a layer. Used to meter network progress accurately — bytes read
// here track exactly against LayerInfo.Size. Caller must gunzip + close.
func (m *Manifest) CompressedLayerReader(digest string) (io.ReadCloser, error) {
	for _, l := range m.layersByDigest() {
		d, err := l.Digest()
		if err != nil {
			return nil, err
		}
		if d.String() == digest {
			return l.Compressed()
		}
	}
	return nil, fmt.Errorf("layer %s not found in manifest", digest)
}

// layersByDigest returns the underlying v1.Layer slice in order.
func (m *Manifest) layersByDigest() []v1.Layer {
	if m.image == nil {
		return nil
	}
	layers, err := m.image.Layers()
	if err != nil {
		return nil
	}
	return layers
}
