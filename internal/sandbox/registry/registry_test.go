package registry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ggcrname "github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// startRegistry spins up an in-process OCI registry. Returns the host:port and a teardown func.
func startRegistry(t *testing.T) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(ggcrregistry.New())
	u, err := url.Parse(srv.URL)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	return u.Host, srv.Close
}

// pushSingleArchAmd64 pushes a tiny amd64 single-arch image. Returns the ref string.
func pushSingleArchAmd64(t *testing.T, host, repo, tag string) string {
	t.Helper()
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ref := host + "/" + repo + ":" + tag
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}
	return ref
}

// pushArmOnly pushes a single-arch arm64 image (used to test the rejection path).
func pushArmOnly(t *testing.T, host, repo, tag string) string {
	t.Helper()
	img, err := random.Image(32, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "arm64"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ref := host + "/" + repo + ":" + tag
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}
	return ref
}

// pushIndex pushes a multi-arch index containing the given platform variants.
// Each platform gets a tiny image whose config file embeds os/arch.
func pushIndex(t *testing.T, host, repo, tag string, platforms []v1.Platform) string {
	t.Helper()

	var addends []mutate.IndexAddendum
	for _, p := range platforms {
		img, err := random.Image(48, 1)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Fatal(err)
		}
		cfg.OS = p.OS
		cfg.Architecture = p.Architecture
		img, err = mutate.ConfigFile(img, cfg)
		if err != nil {
			t.Fatal(err)
		}
		addends = append(addends, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &p},
		})
	}
	idx := mutate.AppendManifests(empty.Index, addends...)

	ref := host + "/" + repo + ":" + tag
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteIndex(parsed, idx); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestPuller_Resolve_SingleArchAmd64(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	ref := pushSingleArchAmd64(t, host, "test/img", "v1")

	manifest, err := New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if manifest.Digest == "" {
		t.Errorf("digest empty")
	}
	if len(manifest.Layers) != 1 {
		t.Errorf("layers = %d want 1", len(manifest.Layers))
	}
	if manifest.Config.Architecture != "amd64" {
		t.Errorf("arch = %s want amd64", manifest.Config.Architecture)
	}
}

func TestPuller_Resolve_RejectArmOnly(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	ref := pushArmOnly(t, host, "test/arm", "v1")

	_, err := New().Resolve(context.Background(), ref)
	if !errors.Is(err, ErrNoAmd64Variant) {
		t.Fatalf("expected ErrNoAmd64Variant, got %v", err)
	}
}

func TestPuller_Resolve_IndexPicksAmd64(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	ref := pushIndex(t, host, "test/multi", "v1", []v1.Platform{
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "ppc64le"},
	})

	manifest, err := New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if manifest.Config.Architecture != "amd64" {
		t.Errorf("picked arch = %s want amd64", manifest.Config.Architecture)
	}
}

func TestPuller_Resolve_IndexNoAmd64Variant(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	ref := pushIndex(t, host, "test/noamd", "v1", []v1.Platform{
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "ppc64le"},
	})

	_, err := New().Resolve(context.Background(), ref)
	if !errors.Is(err, ErrNoAmd64Variant) {
		t.Fatalf("expected ErrNoAmd64Variant, got %v", err)
	}
}

// TestPuller_LayerReader pushes an image with a known tar layer and verifies
// LayerReader returns the same bytes uncompressed.
func TestPuller_LayerReader(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	// Build a layer containing one file "marker" with body "ok".
	tarBuf := buildSingleFileTar(t, "marker", "ok")
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarBuf)), nil
	}, tarball.WithMediaType(types.DockerLayer))
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ref := host + "/test/layer:v1"
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}

	manifest, err := New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Layers) != 1 {
		t.Fatalf("layers = %d want 1", len(manifest.Layers))
	}

	rc, err := manifest.LayerReader(manifest.Layers[0].Digest)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "marker") || !strings.Contains(string(body), "ok") {
		t.Errorf("layer body missing markers; got %q", body)
	}
}

// buildSingleFileTar writes a tar with one regular file.
func buildSingleFileTar(t *testing.T, name, body string) []byte {
	t.Helper()
	// Avoid pulling archive/tar import here; reuse via inline helper would duplicate.
	// Use a goroutine pipe to keep the helper minimal.
	return tarBytesFor(t, name, body)
}
