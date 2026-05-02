package imageconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestLoad_Missing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("expected nil err on missing, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected zero-value config, got nil")
	}
	if len(cfg.Env) != 0 || cfg.WorkDir != "" || cfg.User != "" {
		t.Errorf("expected zero-value config, got %+v", cfg)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etc", "pyro", "image-config.json")

	want := &ImageConfig{
		Env:     []string{"PATH=/x:/y", "PYTHONUNBUFFERED=1"},
		WorkDir: "/app",
		User:    "1000:1000",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("roundtrip mismatch\n got=%+v\nwant=%+v", got, want)
	}
}

func TestMergeEnv_OverrideAndPreserve(t *testing.T) {
	image := []string{"PATH=/usr/local/bin", "PYTHONUNBUFFERED=1", "FOO=bar"}
	req := map[string]string{"FOO": "baz", "EXTRA": "1"}

	got := MergeEnv(image, req)
	sort.Strings(got)
	want := []string{"EXTRA=1", "FOO=baz", "PATH=/usr/local/bin", "PYTHONUNBUFFERED=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge mismatch\n got=%v\nwant=%v", got, want)
	}
}

func TestMergeEnv_EmptyImage(t *testing.T) {
	got := MergeEnv(nil, map[string]string{"X": "y"})
	if !reflect.DeepEqual(got, []string{"X=y"}) {
		t.Errorf("got %v", got)
	}
}

func TestMergeEnv_EmptyReq(t *testing.T) {
	got := MergeEnv([]string{"A=1"}, nil)
	if !reflect.DeepEqual(got, []string{"A=1"}) {
		t.Errorf("got %v", got)
	}
}

func TestResolveCwd(t *testing.T) {
	img := &ImageConfig{WorkDir: "/app"}
	if c := ResolveCwd(img, ""); c != "/app" {
		t.Errorf("image default not used, got %q", c)
	}
	if c := ResolveCwd(img, "/tmp"); c != "/tmp" {
		t.Errorf("request override failed, got %q", c)
	}
	if c := ResolveCwd(nil, ""); c != "" {
		t.Errorf("nil image should return empty, got %q", c)
	}
	if c := ResolveCwd(&ImageConfig{}, ""); c != "" {
		t.Errorf("empty image should return empty, got %q", c)
	}
}

func TestResolveUID_NumericApplied(t *testing.T) {
	uid, ok, fb := ResolveUID("1000", nil)
	if !ok || fb || uid != 1000 {
		t.Errorf("uid=%d ok=%v fb=%v", uid, ok, fb)
	}
}

func TestResolveUID_NumericWithGid(t *testing.T) {
	uid, ok, fb := ResolveUID("1234:5678", nil)
	if !ok || fb || uid != 1234 {
		t.Errorf("uid=%d ok=%v fb=%v", uid, ok, fb)
	}
}

func TestResolveUID_NameResolved(t *testing.T) {
	lookup := func(name string) (int, bool) {
		if name == "appuser" {
			return 5000, true
		}
		return 0, false
	}
	uid, ok, fb := ResolveUID("appuser", lookup)
	if !ok || fb || uid != 5000 {
		t.Errorf("uid=%d ok=%v fb=%v", uid, ok, fb)
	}
}

func TestResolveUID_NameMissing_FallsBack(t *testing.T) {
	lookup := func(string) (int, bool) { return 0, false }
	uid, ok, fb := ResolveUID("ghost", lookup)
	if ok || !fb || uid != 0 {
		t.Errorf("expected fallback to root: uid=%d ok=%v fb=%v", uid, ok, fb)
	}
}

func TestResolveUID_Empty(t *testing.T) {
	uid, ok, fb := ResolveUID("", nil)
	if ok || fb || uid != 0 {
		t.Errorf("uid=%d ok=%v fb=%v", uid, ok, fb)
	}
}
