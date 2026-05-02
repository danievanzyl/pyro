package registry

import (
	"context"
	"reflect"
	"testing"

	ggcrname "github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestExtractLabels_PopulatesFromManifest(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	img, err := random.Image(32, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	cfg.Config = v1.Config{
		Labels: map[string]string{
			"org.opencontainers.image.source":   "https://github.com/example/repo",
			"org.opencontainers.image.revision": "abc123",
			"org.opencontainers.image.created":  "2026-04-30T00:00:00Z",
		},
	}
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ref := host + "/test/labels:v1"
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}

	manifest, err := New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got := ExtractLabels(manifest)
	want := map[string]string{
		"org.opencontainers.image.source":   "https://github.com/example/repo",
		"org.opencontainers.image.revision": "abc123",
		"org.opencontainers.image.created":  "2026-04-30T00:00:00Z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels: got=%v want=%v", got, want)
	}
}

func TestExtractLabels_NilManifest(t *testing.T) {
	if got := ExtractLabels(nil); got != nil {
		t.Errorf("nil manifest: got %v want nil", got)
	}
}

func TestExtractLabels_EmptyLabels(t *testing.T) {
	host, stop := startRegistry(t)
	defer stop()

	img, err := random.Image(32, 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	// No labels set.
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ref := host + "/test/nolabels:v1"
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}

	manifest, err := New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got := ExtractLabels(manifest); got != nil {
		t.Errorf("empty labels: got %v want nil", got)
	}
}
