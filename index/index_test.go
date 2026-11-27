package index

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func fixture() []Doc {
	return []Doc{
		{Address: "https://example.org/b.md", Title: "Gardening notes", Description: "Soil and seeds", Body: "# Soil\n\nTomatoes like warm soil. Seeds go in early."},
		{Address: "https://example.org/a.md", Title: "Search engines", Description: "How a search index works", Body: "An index maps words to pages. Tomatoes are not mentioned here, only a garden of words."},
		{Address: "https://example.org/c.md", Title: "Untitled", Body: "Nothing about the topic. The quick brown fox."},
	}
}

func build(t *testing.T, docs []Doc, size int) string {
	t.Helper()
	dir := t.TempDir()
	if err := Build(dir, docs, BuildOptions{ChunkEntries: size, Generation: 1, Built: fixedTime}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func open(t *testing.T, dir string) *Index {
	t.Helper()
	ix, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

// search parses q and runs it: the path a command takes.
func search(ix *Index, q string, limit int) ([]Hit, error) {
	n, err := ParseQuery(q, nil)
	if err != nil {
		return nil, err
	}
	return ix.Search(n, limit)
}

func addrs(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Address)
	}
	return out
}

func TestRoundTripTermSearch(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	if ix.Count() != 3 {
		t.Fatalf("count = %d", ix.Count())
	}
	hits, err := search(ix, "tomatoes", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %v", addrs(hits))
	}
	// the page that is about gardening is shorter and says it first: ranks higher or equal; both found
	got := map[string]bool{}
	for _, h := range hits {
		got[h.Address] = true
	}
	if !got["https://example.org/a.md"] || !got["https://example.org/b.md"] {
		t.Fatalf("wrong pages %v", addrs(hits))
	}
}

func TestImplicitAndAndNoMatch(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	hits, _ := search(ix, "tomatoes seeds", 10)
	if !reflect.DeepEqual(addrs(hits), []string{"https://example.org/b.md"}) {
		t.Fatalf("AND = %v", addrs(hits))
	}
	hits, err := search(ix, "zebra", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("no match = %v, %v", hits, err)
	}
}

func TestTitleOutweighsBody(t *testing.T) {
	docs := []Doc{
		{Address: "https://x/body.md", Title: "Other", Body: "alpha beta gamma"},
		{Address: "https://x/title.md", Title: "Alpha", Body: "beta gamma delta"},
	}
	ix := open(t, build(t, docs, 0))
	hits, _ := search(ix, "alpha", 10)
	if len(hits) != 2 || hits[0].Address != "https://x/title.md" {
		t.Fatalf("order = %v", addrs(hits))
	}
}

func TestPrefixAcrossChunks(t *testing.T) {
	// a tiny chunk size forces many chunks so a prefix must read a range of them
	ix := open(t, build(t, fixture(), 3))
	if ix.Chunks() < 3 {
		t.Fatalf("expected several chunks, got %d", ix.Chunks())
	}
	hits, err := search(ix, "tomato*", 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("prefix hits = %v, %v", addrs(hits), err)
	}
	hits, _ = search(ix, "gard*", 10)
	if len(hits) != 2 { // "Gardening" in b, "garden" in a
		t.Fatalf("gard* = %v", addrs(hits))
	}
}

func TestPhrase(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	hits, _ := search(ix, `"quick brown fox"`, 10)
	if !reflect.DeepEqual(addrs(hits), []string{"https://example.org/c.md"}) {
		t.Fatalf("phrase = %v", addrs(hits))
	}
	hits, _ = search(ix, `"brown quick"`, 10)
	if len(hits) != 0 {
		t.Fatalf("reversed phrase matched %v", addrs(hits))
	}
	// words of title and body must not join into a phrase across the boundary
	hits, _ = search(ix, `"untitled nothing"`, 10)
	if len(hits) != 0 {
		t.Fatalf("phrase crossed fields: %v", addrs(hits))
	}
}

func TestFragmentWordCountInvariant(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	hits, _ := search(ix, "soil", 10)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	f, err := ix.Fragment(hits[0].Doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(f.Text)) != f.WordCount {
		t.Fatalf("fragment has %d words, says %d", len(strings.Fields(f.Text)), f.WordCount)
	}
	if f.Address != hits[0].Address || f.Title != "Gardening notes" {
		t.Fatalf("fragment = %+v", f)
	}
}

func TestDeterministicBytes(t *testing.T) {
	a, b := build(t, fixture(), 5), build(t, fixture(), 5)
	// input order must not matter either
	rev := fixture()
	rev[0], rev[2] = rev[2], rev[0]
	c := build(t, rev, 5)
	for _, other := range []string{b, c} {
		if !sameTree(t, a, other) {
			t.Fatal("same input produced different files")
		}
	}
}

func sameTree(t *testing.T, a, b string) bool {
	t.Helper()
	var fa, fb []string
	for _, p := range []struct {
		dir string
		out *[]string
	}{{a, &fa}, {b, &fb}} {
		filepath.Walk(p.dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(p.dir, path)
				data, _ := os.ReadFile(path)
				*p.out = append(*p.out, rel+"\x00"+string(data))
			}
			return nil
		})
	}
	return reflect.DeepEqual(fa, fb)
}

func TestVerifyCatchesCorruption(t *testing.T) {
	dir := build(t, fixture(), 0)
	if err := Verify(dir); err != nil {
		t.Fatalf("clean index failed verify: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "w", "*"))
	if len(files) == 0 {
		t.Fatal("no word chunks")
	}
	if err := os.WriteFile(files[0], []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Verify(dir)
	if err == nil {
		t.Fatal("corruption not detected")
	}
	if !IsDataError(err) {
		t.Fatalf("want a data error (exit 65), got %v", err)
	}
	// Open reads lazily; a search over the damaged chunk must fail, not return wrong hits
	ix := open(t, dir)
	for _, w := range []string{"tomatoes", "index", "soil", "quick", "gardening"} {
		if _, err := search(ix, w, 10); err != nil {
			return
		}
	}
	t.Fatal("no search over the corrupted chunk failed")
}

func TestOpenRefusesUnknownFormatAndAnalyzer(t *testing.T) {
	for _, edit := range [][2]string{{`"cmarkfind/1"`, `"cmarkfind/2"`}, {`"unicode-word-v1"`, `"nfkc-v9"`}} {
		dir := build(t, fixture(), 0)
		p := filepath.Join(dir, "manifest.json")
		data, _ := os.ReadFile(p)
		if !strings.Contains(string(data), edit[0]) {
			t.Fatalf("manifest lacks %s", edit[0])
		}
		os.WriteFile(p, []byte(strings.Replace(string(data), edit[0], edit[1], 1)), 0o644)
		if _, err := Open(dir); err == nil || !IsDataError(err) {
			t.Fatalf("edit %v: err = %v", edit, err)
		}
	}
}

func TestOpenMissingAndBadDigest(t *testing.T) {
	dir := build(t, fixture(), 0)
	p := filepath.Join(dir, "manifest.json")
	data, _ := os.ReadFile(p)
	// a digest turned into a path escape must be refused, not followed
	i := strings.Index(string(data), `"documents"`)
	bad := string(data[:i]) + strings.Replace(string(data[i:]), `"ranges": [`, `"ranges": [[0,0,["../../etc/passwd",1]],`, 1)
	os.WriteFile(p, []byte(bad), 0o644)
	if _, err := Open(dir); err == nil || !IsDataError(err) {
		t.Fatalf("path-escape digest: %v", err)
	}
	if _, err := Open(filepath.Join(dir, "nope")); err == nil || !IsMissing(err) {
		t.Fatalf("missing dir: %v", err)
	}
}
