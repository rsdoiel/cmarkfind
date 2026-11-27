package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	n, err := PruneOlderThan(dir, 0)
	if err != nil || n == 0 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("index damaged by prune: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a file that is not an index file was removed")
	}
	if n2, _ := PruneOlderThan(dir, 0); n2 != 0 {
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

func backdate(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
}

// A search that opened the index before a rebuild reads files lazily; a prune
// that removes what the new manifest no longer names breaks it.
func TestPruneKeepsFilesTheLastBuildDroppedUntilTheyAreOld(t *testing.T) {
	dir := t.TempDir()
	a := []Doc{{Address: "https://e.org/a", Title: "Otters", Body: "river otter"}}
	b := []Doc{{Address: "https://e.org/b", Title: "Cranes", Body: "shallow water"}}
	if err := Build(dir, a, BuildOptions{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(dir) // a search in flight
	if err != nil {
		t.Fatal(err)
	}
	if err := Build(dir, b, BuildOptions{Generation: 2}); err != nil {
		t.Fatal(err)
	}
	n, err := Prune(dir)
	if err != nil || n != 0 {
		t.Fatalf("a prune right after a build removed %d files (%v)", n, err)
	}
	q, _ := ParseQuery("otter", nil)
	hits, err := reader.Search(q, 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("the reader of the old generation: %v %v", hits, err)
	}
	backdate(t, dir, time.Hour)
	if n, err := Prune(dir); err != nil || n == 0 {
		t.Fatalf("old leftovers not pruned: %d %v", n, err)
	}
	if err := Verify(dir); err != nil {
		t.Fatal(err)
	}
}

// Readers that open the index while it is rebuilt see a whole manifest and
// every file it names, never a half-written one.
func TestReadersNeverSeeAHalfBuiltIndexWhileItIsRebuilt(t *testing.T) {
	dir := t.TempDir()
	mk := func(g int) []Doc {
		docs := []Doc{{Address: "https://e.org/stable", Title: "Stable", Body: "otter river"}}
		for i := 0; i < 40; i++ {
			docs = append(docs, Doc{Address: "https://e.org/p" + string(rune('a'+i%26)) + string(rune('a'+g%26)) + string(rune('a'+i/26)), Title: "Page", Body: "otter filler words number " + string(rune('a'+g%26))})
		}
		return docs
	}
	if err := Build(dir, mk(0), BuildOptions{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		q, _ := ParseQuery("otter", nil)
		for {
			select {
			case <-stop:
				return
			default:
			}
			ix, err := Open(dir)
			if err == nil {
				_, err = ix.Search(q, 0)
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}()
	for g := 2; g < 30; g++ {
		if err := Build(dir, mk(g), BuildOptions{ChunkEntries: 50, Generation: uint64(g)}); err != nil {
			t.Fatal(err)
		}
		if _, err := Prune(dir); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	if err := <-errs; err != nil {
		t.Fatalf("a reader failed during a rebuild: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "manifest.json" && e.Name() != "w" && e.Name() != "d" && e.Name() != "f" {
			t.Errorf("stray file %q left in the index directory", e.Name())
		}
	}
}

// A build that died between creating a temporary file and renaming it leaves
// `.tmp-*` behind; Prune clears those once they are old, in the root and in w,
// d and f, and never touches a recent one (a build may be writing it now).
func TestPruneClearsOldTemporaryFilesAndKeepsRecentOnes(t *testing.T) {
	dir := t.TempDir()
	if err := Build(dir, []Doc{{Address: "https://e.org/a", Title: "A", Body: "otter"}}, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	old, recent := []string{".tmp-old1", "w/.tmp-old2", "d/.tmp-old3", "f/.tmp-old4"}, []string{".tmp-new1", "w/.tmp-new2"}
	for _, n := range append(append([]string{}, old...), recent...) {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(n)), []byte("partial"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stray := filepath.Join(dir, "notes.txt")
	os.WriteFile(stray, []byte("mine"), 0o644)
	hour := time.Now().Add(-time.Hour)
	for _, n := range old {
		os.Chtimes(filepath.Join(dir, filepath.FromSlash(n)), hour, hour)
	}
	os.Chtimes(stray, hour, hour)
	n, err := Prune(dir)
	if err != nil || n != len(old) {
		t.Fatalf("pruned %d (%v), want %d", n, err, len(old))
	}
	for _, f := range old {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
			t.Errorf("%s was left", f)
		}
	}
	for _, f := range append(recent, "notes.txt") {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s was removed", f)
		}
	}
	if err := Verify(dir); err != nil {
		t.Error(err)
	}
}
