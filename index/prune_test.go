package index

import (
	"os"
	"path/filepath"
	"testing"
)

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestPruneRemovesOnlyFilesTheManifestDoesNotName(t *testing.T) {
	dir := t.TempDir()
	a := []Doc{{Address: "https://e.org/a", Title: "Otters", Body: "river otter"}}
	b := []Doc{{Address: "https://e.org/a", Title: "Otters", Body: "river otter dives"}, {Address: "https://e.org/b", Title: "Cranes", Body: "water"}}
	if err := Build(dir, a, BuildOptions{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	one := countFiles(t, dir)
	if err := Build(dir, b, BuildOptions{Generation: 2}); err != nil {
		t.Fatal(err)
	}
	if countFiles(t, dir) <= one {
		t.Fatal("expected the second build to leave the first one's files")
	}
	stray := filepath.Join(dir, "notes.txt")
	os.WriteFile(stray, []byte("mine"), 0o644)
	n, err := Prune(dir)
	if err != nil || n == 0 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("index damaged by prune: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a file that is not an index file was removed")
	}
	if n2, _ := Prune(dir); n2 != 0 {
		t.Errorf("second prune removed %d", n2)
	}
}

func TestPruneRefusesADirectoryWithoutAManifest(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "w"), 0o755)
	f := filepath.Join(dir, "w", "x.cbor.gz")
	os.WriteFile(f, []byte("x"), 0o644)
	if _, err := Prune(dir); !IsMissing(err) {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Error("file removed from a directory that is not an index")
	}
}
