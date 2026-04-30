package registry

import (
	"context"
	"reflect"
	"sort"
	"testing"

	ggcrname "github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestExtractConfig_PopulatesEnvWorkdirUser(t *testing.T) {
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
		Env:        []string{"PATH=/usr/local/bin:/usr/bin", "PYTHONUNBUFFERED=1"},
		WorkingDir: "/app",
		User:       "1000:1000",
	}
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ref := host + "/test/cfg:v1"
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

	got := ExtractConfig(manifest)
	wantEnv := []string{"PATH=/usr/local/bin:/usr/bin", "PYTHONUNBUFFERED=1"}
	sort.Strings(got.Env)
	sort.Strings(wantEnv)
	if !reflect.DeepEqual(got.Env, wantEnv) {
		t.Errorf("env: got=%v want=%v", got.Env, wantEnv)
	}
	if got.WorkDir != "/app" {
		t.Errorf("workdir=%q want=/app", got.WorkDir)
	}
	if got.User != "1000:1000" {
		t.Errorf("user=%q want=1000:1000", got.User)
	}
}

func TestExtractConfig_NilManifest(t *testing.T) {
	got := ExtractConfig(nil)
	if len(got.Env) != 0 || got.WorkDir != "" || got.User != "" {
		t.Errorf("expected zero, got %+v", got)
	}
}
