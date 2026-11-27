package index

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// memSource copies an index directory into memory: a source that is not a directory.
func memSource(t *testing.T, dir string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		m[filepath.ToSlash(rel)] = &fstest.MapFile{Data: b}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOpenSourceSearchesWithoutADirectory(t *testing.T) {
	src := memSource(t, build(t, fixture(), 0))
	ix, err := OpenSource(src)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search(ix, "tomatoes", 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits %v, err %v", addrs(hits), err)
	}
	n, _ := ParseQuery("tomatoes", nil)
	if e, err := ix.Excerpt(n, hits[0].Doc, 0); err != nil || e.HTML == "" {
		t.Fatalf("excerpt %+v, err %v", e, err)
	}
}

func TestOpenSourceMissingManifestIsMissing(t *testing.T) {
	_, err := OpenSource(fstest.MapFS{})
	if !IsMissing(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenSourceMissingFileIsDataError(t *testing.T) {
	src := memSource(t, build(t, fixture(), 0))
	for name := range src {
		if name != "manifest.json" {
			delete(src, name)
			break
		}
	}
	ix, err := OpenSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyIndex(ix); err == nil || !IsDataError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenSourceOtherErrorsAreNotMissing(t *testing.T) {
	_, err := OpenSource(failing{})
	if err == nil || IsMissing(err) || IsDataError(err) {
		t.Fatalf("err = %v", err)
	}
}

type failing struct{}

func (failing) Open(string) (fs.File, error) { return nil, errors.New("network down") }
