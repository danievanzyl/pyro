package imageops

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// tarBuilder helps assemble in-memory tar streams for tests.
type tarBuilder struct {
	buf bytes.Buffer
	tw  *tar.Writer
}

func newTarBuilder() *tarBuilder {
	tb := &tarBuilder{}
	tb.tw = tar.NewWriter(&tb.buf)
	return tb
}

func (tb *tarBuilder) addFile(name string, body string, mode int64) {
	hdr := &tar.Header{
		Name:     name,
		Mode:     mode,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	if err := tb.tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
	if _, err := tb.tw.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (tb *tarBuilder) addDir(name string, mode int64) {
	hdr := &tar.Header{
		Name:     name,
		Mode:     mode,
		Typeflag: tar.TypeDir,
	}
	if err := tb.tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
}

func (tb *tarBuilder) addSymlink(name, link string) {
	hdr := &tar.Header{
		Name:     name,
		Linkname: link,
		Typeflag: tar.TypeSymlink,
	}
	if err := tb.tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
}

func (tb *tarBuilder) addEmpty(name string) {
	hdr := &tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
	}
	if err := tb.tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
}

func (tb *tarBuilder) reader() io.Reader {
	if err := tb.tw.Close(); err != nil {
		panic(err)
	}
	return bytes.NewReader(tb.buf.Bytes())
}

func TestLayerExtractor_BasicFiles(t *testing.T) {
	root := t.TempDir()

	tb := newTarBuilder()
	tb.addDir("etc/", 0o755)
	tb.addFile("etc/hello", "hello world", 0o644)
	tb.addFile("usr/bin/python", "#!/bin/sh\necho hi", 0o755)

	if err := NewLayerExtractor().Extract(root, tb.reader()); err != nil {
		t.Fatalf("extract: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, "etc/hello"))
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if string(body) != "hello world" {
		t.Errorf("hello body = %q", body)
	}

	info, err := os.Stat(filepath.Join(root, "usr/bin/python"))
	if err != nil {
		t.Fatalf("stat python: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("python mode = %v want 0755", info.Mode().Perm())
	}
}

func TestLayerExtractor_Whiteout(t *testing.T) {
	root := t.TempDir()

	// Layer 1: create files.
	l1 := newTarBuilder()
	l1.addFile("etc/keep", "keep", 0o644)
	l1.addFile("etc/remove", "remove", 0o644)

	ex := NewLayerExtractor()
	if err := ex.Extract(root, l1.reader()); err != nil {
		t.Fatalf("layer 1: %v", err)
	}

	// Layer 2: whiteout etc/remove.
	l2 := newTarBuilder()
	l2.addEmpty("etc/.wh.remove")
	if err := ex.Extract(root, l2.reader()); err != nil {
		t.Fatalf("layer 2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "etc/keep")); err != nil {
		t.Errorf("etc/keep should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/remove")); !os.IsNotExist(err) {
		t.Errorf("etc/remove should be removed, got err=%v", err)
	}
}

func TestLayerExtractor_OpaqueDir(t *testing.T) {
	root := t.TempDir()

	// Layer 1: populate /var/cache.
	l1 := newTarBuilder()
	l1.addDir("var/cache/", 0o755)
	l1.addFile("var/cache/a", "a", 0o644)
	l1.addFile("var/cache/b", "b", 0o644)
	l1.addFile("var/keepme", "keep", 0o644)

	ex := NewLayerExtractor()
	if err := ex.Extract(root, l1.reader()); err != nil {
		t.Fatalf("layer 1: %v", err)
	}

	// Layer 2: opaque-dir on var/cache plus a fresh entry.
	l2 := newTarBuilder()
	l2.addEmpty("var/cache/.wh..wh..opq")
	l2.addFile("var/cache/new", "new", 0o644)
	if err := ex.Extract(root, l2.reader()); err != nil {
		t.Fatalf("layer 2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "var/cache/a")); !os.IsNotExist(err) {
		t.Errorf("var/cache/a should be cleared")
	}
	if _, err := os.Stat(filepath.Join(root, "var/cache/b")); !os.IsNotExist(err) {
		t.Errorf("var/cache/b should be cleared")
	}
	if body, err := os.ReadFile(filepath.Join(root, "var/cache/new")); err != nil || string(body) != "new" {
		t.Errorf("var/cache/new missing or wrong: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(root, "var/keepme")); err != nil {
		t.Errorf("var/keepme outside opaque scope must remain: %v", err)
	}
}

func TestLayerExtractor_Symlink(t *testing.T) {
	root := t.TempDir()
	tb := newTarBuilder()
	tb.addFile("usr/bin/python3.12", "real", 0o755)
	tb.addSymlink("usr/bin/python", "python3.12")

	if err := NewLayerExtractor().Extract(root, tb.reader()); err != nil {
		t.Fatalf("extract: %v", err)
	}
	target, err := os.Readlink(filepath.Join(root, "usr/bin/python"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "python3.12" {
		t.Errorf("symlink target = %q", target)
	}
}

func TestLayerExtractor_RejectPathEscape(t *testing.T) {
	root := t.TempDir()
	tb := newTarBuilder()
	tb.addFile("../escape", "x", 0o644)
	// Path is cleaned to /escape -> root/escape, which is fine. Use an absolute-traversal style.
	// Verify the cleaner keeps everything inside root.
	if err := NewLayerExtractor().Extract(root, tb.reader()); err != nil {
		t.Fatalf("extract: %v", err)
	}
	// The cleaned path becomes root/escape; nothing escaped.
	if _, err := os.Stat(filepath.Join(root, "escape")); err != nil {
		t.Errorf("expected file inside root: %v", err)
	}
	parent := filepath.Dir(root)
	if _, err := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(err) {
		t.Errorf("file leaked outside root: err=%v", err)
	}
}
