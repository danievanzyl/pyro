package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/danievanzyl/pyro/internal/sandbox/imageops"
	"github.com/danievanzyl/pyro/internal/sandbox/imagestate"
	"github.com/danievanzyl/pyro/internal/sandbox/registry"
	ggcrname "github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// captureEmitter records every event published, thread-safe.
type captureEmitter struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	Type    string
	Payload map[string]any
}

func (c *captureEmitter) Publish(eventType string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, _ := data.(map[string]any)
	c.events = append(c.events, capturedEvent{Type: eventType, Payload: m})
}

func (c *captureEmitter) snapshot() []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedEvent, len(c.events))
	copy(out, c.events)
	return out
}

// pushLayerImage pushes a tiny single-arch amd64 image with one tar layer
// of the given byte payload. Returns the registry ref.
func pushLayerImage(t *testing.T, host, repo, tag string, layerBytes []byte) string {
	t.Helper()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerBytes)), nil
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

// buildTar packages a single regular file with the given body.
func buildTar(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractLayer_EmitsProgressMonotonic exercises the orchestrator's
// per-layer download+extract path with a real httptest OCI registry. It
// verifies image.layer_progress events fire with monotonically
// increasing bytes_done and a final value matching layer.Size. Runs on
// macOS — extractLayer doesn't touch ext4.
func TestExtractLayer_EmitsProgressMonotonic(t *testing.T) {
	srv := httptest.NewServer(ggcrregistry.New())
	defer srv.Close()
	host, err := hostOf(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Make a layer ~3 MiB so we cross the 1 MiB byte threshold a few times.
	payload := bytes.Repeat([]byte{'x'}, 3<<20)
	tarBytes := buildTar(t, "marker", payload)
	ref := pushLayerImage(t, host, "test/progress", "v1", tarBytes)

	manifest, err := registry.New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(manifest.Layers) != 1 {
		t.Fatalf("layers = %d want 1", len(manifest.Layers))
	}

	emitter := &captureEmitter{}
	im := &ImageManager{
		cfg:     ImageConfig{ImagesDir: t.TempDir()},
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ledger:  imagestate.New(nil, time.Hour),
		emitter: emitter,
	}

	dst := t.TempDir()
	extractor := imageops.NewLayerExtractor()
	if err := im.extractLayer("img", manifest.Layers[0], manifest, extractor, dst); err != nil {
		t.Fatalf("extractLayer: %v", err)
	}

	// Verify file extracted.
	body, err := os.ReadFile(dst + "/marker")
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if len(body) != len(payload) {
		t.Errorf("marker size = %d want %d", len(body), len(payload))
	}

	events := emitter.snapshot()
	if len(events) == 0 {
		t.Fatalf("no progress events emitted")
	}

	var lastDone int64
	var lastDigest string
	layerSize := manifest.Layers[0].Size
	layerDigest := manifest.Layers[0].Digest
	for i, ev := range events {
		if ev.Type != imagestate.EventLayerProgress {
			t.Errorf("event[%d] type = %s want %s", i, ev.Type, imagestate.EventLayerProgress)
			continue
		}
		if ev.Payload["name"] != "img" {
			t.Errorf("event[%d] name = %v", i, ev.Payload["name"])
		}
		ld, _ := ev.Payload["layer_digest"].(string)
		if ld != layerDigest {
			t.Errorf("event[%d] layer_digest = %s want %s", i, ld, layerDigest)
		}
		lastDigest = ld
		done, _ := ev.Payload["bytes_done"].(int64)
		total, _ := ev.Payload["bytes_total"].(int64)
		if total != layerSize {
			t.Errorf("event[%d] bytes_total = %d want %d", i, total, layerSize)
		}
		if done < lastDone {
			t.Errorf("event[%d] bytes_done %d < prior %d (not monotonic)", i, done, lastDone)
		}
		lastDone = done
	}
	if lastDigest == "" {
		t.Errorf("layer digest never set")
	}
	// Final emit guarantees bytes_done == bytes_total.
	if lastDone != layerSize {
		t.Errorf("final bytes_done = %d want %d", lastDone, layerSize)
	}
}

// TestExtractLayer_SmallLayerFinalEmitOnly verifies a tiny layer (<1 MiB
// downloaded fast) emits at most a couple of progress events — the
// final emit always fires, so the lower bound is 1.
func TestExtractLayer_SmallLayerFinalEmitOnly(t *testing.T) {
	srv := httptest.NewServer(ggcrregistry.New())
	defer srv.Close()
	host, err := hostOf(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	tarBytes := buildTar(t, "tiny", []byte("ok"))
	ref := pushLayerImage(t, host, "test/tiny", "v1", tarBytes)

	manifest, err := registry.New().Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	emitter := &captureEmitter{}
	im := &ImageManager{
		cfg:     ImageConfig{ImagesDir: t.TempDir()},
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ledger:  imagestate.New(nil, time.Hour),
		emitter: emitter,
	}

	dst := t.TempDir()
	if err := im.extractLayer("img", manifest.Layers[0], manifest, imageops.NewLayerExtractor(), dst); err != nil {
		t.Fatalf("extractLayer: %v", err)
	}

	events := emitter.snapshot()
	if len(events) == 0 || len(events) > 3 {
		t.Errorf("small layer event count = %d, expected 1–3", len(events))
	}
	for _, ev := range events {
		if ev.Type != imagestate.EventLayerProgress {
			t.Errorf("unexpected event type: %s", ev.Type)
		}
	}
}

// TestProgressReader_FlushFinalEmitsTotalOnEarlyEOF guards against the
// case where the on-the-wire bytes are fewer than layer.Size — the
// final emit clamps bytes_done up to bytes_total so consumers don't
// see bytes_done<bytes_total at completion.
func TestProgressReader_FlushFinalEmitsTotalOnEarlyEOF(t *testing.T) {
	got := []int64{}
	pr := newProgressReader(bytes.NewReader([]byte("hi")), 100, func(d int64) {
		got = append(got, d)
	})
	_, _ = io.ReadAll(pr)
	pr.flushFinal()

	if len(got) == 0 {
		t.Fatalf("flushFinal must emit at least once")
	}
	last := got[len(got)-1]
	if last != 100 {
		t.Errorf("final bytes_done = %d want 100 (clamped to total)", last)
	}
}

// TestProgressReader_NoEmitWithoutBytes ensures Read with zero bytes
// does not trigger an emit (only the explicit flushFinal does).
func TestProgressReader_NoEmitWithoutBytes(t *testing.T) {
	called := 0
	pr := newProgressReader(bytes.NewReader(nil), 50, func(d int64) {
		called++
	})
	_, _ = io.ReadAll(pr)
	if called != 0 {
		t.Errorf("emit fired without bytes; called=%d", called)
	}
	pr.flushFinal()
	if called != 1 {
		t.Errorf("flushFinal should fire once; called=%d", called)
	}
}

// TestCreateFromRegistry_SizeCapRejects pushes a ~3 MiB layer image,
// then sets MaxImageSizeMB=1 and verifies CreateFromRegistry returns
// ErrImageTooLarge synchronously without creating any ledger entry or
// on-disk image directory.
func TestCreateFromRegistry_SizeCapRejects(t *testing.T) {
	srv := httptest.NewServer(ggcrregistry.New())
	defer srv.Close()
	host, err := hostOf(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Random bytes resist gzip compression, so manifest layer size
	// stays close to 3 MiB. A bytes.Repeat payload would compress to
	// kilobytes and slip past the cap.
	payload := make([]byte, 3<<20)
	if _, err := cryptorand.Read(payload); err != nil {
		t.Fatal(err)
	}
	tarBytes := buildTar(t, "marker", payload)
	ref := pushLayerImage(t, host, "test/toolarge", "v1", tarBytes)

	imagesDir := t.TempDir()
	im, err := NewImageManager(ImageConfig{
		ImagesDir:      imagesDir,
		MaxImageSizeMB: 1, // tiny cap forces rejection
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = im.CreateFromRegistry(context.Background(), "toolarge", ref, false, nil)
	if err == nil {
		t.Fatal("expected ErrImageTooLarge, got nil")
	}
	var tooLarge *imageops.ImageTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *ImageTooLargeError, got %T: %v", err, err)
	}
	if tooLarge.LimitMB != 1 {
		t.Errorf("limit = %d want 1", tooLarge.LimitMB)
	}
	if tooLarge.EstimatedMB <= 0 {
		t.Errorf("estimated should be > 0, got %d", tooLarge.EstimatedMB)
	}

	// No ledger entry, no directory.
	if op := im.Ledger().Get("toolarge"); op != nil {
		t.Errorf("ledger should be empty after size-cap reject, got %+v", op)
	}
	if _, err := os.Stat(imagesDir + "/toolarge"); !os.IsNotExist(err) {
		t.Errorf("image dir should not exist after size-cap reject (err=%v)", err)
	}
}

// TestCreateFromRegistry_UnderCapAccepts confirms a small image under
// the cap proceeds past the size check (reaches Begin and starts the
// async pull goroutine).
func TestCreateFromRegistry_UnderCapAccepts(t *testing.T) {
	srv := httptest.NewServer(ggcrregistry.New())
	defer srv.Close()
	host, err := hostOf(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	tarBytes := buildTar(t, "tiny", []byte("ok"))
	ref := pushLayerImage(t, host, "test/undercap", "v1", tarBytes)

	im, err := NewImageManager(ImageConfig{
		ImagesDir:      t.TempDir(),
		MaxImageSizeMB: 4096,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	info, err := im.CreateFromRegistry(context.Background(), "undercap", ref, false, nil)
	if err != nil {
		t.Fatalf("expected pulling response, got err=%v", err)
	}
	if info.Status != imagestate.StatusPulling {
		t.Errorf("status = %s want pulling", info.Status)
	}
}

// fakeInvalidator records Invalidate calls for verification.
type fakeInvalidator struct {
	mu      sync.Mutex
	invoked []string
}

func (f *fakeInvalidator) Invalidate(image string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invoked = append(f.invoked, image)
}

func (f *fakeInvalidator) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.invoked))
	copy(out, f.invoked)
	return out
}

// stageReadyImage writes a minimal on-disk image (rootfs.ext4 +
// vmlinux + image-meta.json) so Get() returns a ready ImageInfo without
// running the pull goroutine. Returns the digest stamped in the meta.
func stageReadyImage(t *testing.T, imagesDir, name, digest, source string) {
	t.Helper()
	dir := imagesDir + "/" + name
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/rootfs.ext4", []byte("rootfs"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/vmlinux", []byte("kernel"), 0o640); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"digest":"` + digest + `","source":"` + source + `"}`)
	if err := os.WriteFile(dir+"/"+imageMetaName, meta, 0o640); err != nil {
		t.Fatal(err)
	}
}

// TestCreateFromRegistry_NoForce_ReturnsExistingNoPull stages a ready
// image on disk, calls CreateFromRegistry without force, and verifies
// the existing image is returned without starting a goroutine or
// touching the ledger.
func TestCreateFromRegistry_NoForce_ReturnsExistingNoPull(t *testing.T) {
	imagesDir := t.TempDir()
	stageReadyImage(t, imagesDir, "py312", "sha256:old", "python:3.12")

	im, err := NewImageManager(ImageConfig{
		ImagesDir:      imagesDir,
		MaxImageSizeMB: -1,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	info, err := im.CreateFromRegistry(context.Background(), "py312", "python:3.12", false, nil)
	if err != nil {
		t.Fatalf("expected idempotent success, got err=%v", err)
	}
	if info.Status != imagestate.StatusReady {
		t.Errorf("status = %s want ready", info.Status)
	}
	if info.Digest != "sha256:old" {
		t.Errorf("digest = %s want sha256:old", info.Digest)
	}
	// No ledger entry — idempotent path skips Begin entirely.
	if op := im.Ledger().Get("py312"); op != nil {
		t.Errorf("ledger should be empty on idempotent path; got %+v", op)
	}
}

// TestCreateFromRegistry_ForceDuringInFlight_409 seeds an in-flight
// pull and verifies a force-replace request returns ErrForceDuringPull
// without invalidating the pool.
func TestCreateFromRegistry_ForceDuringInFlight_409(t *testing.T) {
	im, err := NewImageManager(ImageConfig{
		ImagesDir:      t.TempDir(),
		MaxImageSizeMB: -1,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	inv := &fakeInvalidator{}
	im.SetInvalidator(inv)

	// Seed an in-flight pull.
	im.Ledger().Begin("py312", "python:3.12")

	_, err = im.CreateFromRegistry(context.Background(), "py312", "python:3.12", true, nil)
	if !errors.Is(err, imagestate.ErrForceDuringPull) {
		t.Fatalf("expected ErrForceDuringPull, got %v", err)
	}
	// Pool must not have been touched — the in-flight pull will become
	// ready and own those snapshots.
	if got := inv.calls(); len(got) != 0 {
		t.Errorf("invalidator should not be called when force is rejected; got %v", got)
	}
}

// TestCreateFromRegistry_ForceInvalidatesPool stages a ready image,
// requests force=true, and verifies the invalidator was called with
// the image name BEFORE the pull goroutine starts. The puller is left
// nil so the actual pull will race-fail against the unreachable
// "no:such" source — that's irrelevant; we only assert the
// pre-goroutine pool drop.
func TestCreateFromRegistry_ForceInvalidatesPool(t *testing.T) {
	imagesDir := t.TempDir()
	stageReadyImage(t, imagesDir, "py312", "sha256:old", "python:3.12")

	im, err := NewImageManager(ImageConfig{
		ImagesDir:      imagesDir,
		MaxImageSizeMB: -1, // disable cap so LayerSizes is skipped
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	inv := &fakeInvalidator{}
	im.SetInvalidator(inv)

	// Cap is -1 so LayerSizes is skipped → returns synchronously after
	// Begin spawns the goroutine. Goroutine will fail in Resolve but
	// that's after the synchronous return.
	info, err := im.CreateFromRegistry(context.Background(), "py312", "127.0.0.1:1/no:such", true, nil)
	if err != nil {
		t.Fatalf("expected synchronous pulling response, got err=%v", err)
	}
	if info.Status != imagestate.StatusPulling {
		t.Errorf("status = %s want pulling", info.Status)
	}

	got := inv.calls()
	if len(got) != 1 || got[0] != "py312" {
		t.Errorf("invalidator calls = %v want [py312]", got)
	}
}

func hostOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}
