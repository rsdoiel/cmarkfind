package index

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func manyDocs(n int) []Doc {
	docs := make([]Doc, n)
	for i := range docs {
		docs[i] = Doc{Address: "https://example.org/p/" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".md", Title: "Page", Body: "the quick brown fox number " + string(rune('a'+i%26))}
	}
	return docs
}

// The address of a hit is in the document table, so ranking every page that
// matches reads no page text. (It used to inflate each hit's fragment for the
// address: a search for a common word cost its page count times the page size.)
func TestSearchReadsNoFragmentToLearnAHitsAddress(t *testing.T) {
	var src fstest.MapFS = memSource(t, build(t, manyDocs(60), 0))
	c := &countingFS{FS: src, opened: map[string]int{}}
	ix, err := OpenSource(c)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := search(ix, "the", 0)
	if err != nil || len(hits) != 60 {
		t.Fatalf("hits %d, err %v", len(hits), err)
	}
	if n := c.fragments(); n != 0 {
		t.Errorf("a search for a word on 60 pages read %d fragments", n)
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.Address, "https://example.org/p/") {
			t.Fatalf("hit %d has address %q", h.Doc, h.Address)
		}
	}
	// the same with a limit
	c2 := &countingFS{FS: src, opened: map[string]int{}}
	ix2, _ := OpenSource(c2)
	if hits, _ := search(ix2, "fox", 5); len(hits) != 5 || c2.fragments() != 0 {
		t.Errorf("limit 5: %d hits, %d fragments read", len(hits), c2.fragments())
	}
}

// An index written before rows carried an address still answers, from the
// fragments.
func TestARowWithoutAnAddressFallsBackToTheFragment(t *testing.T) {
	ix := open(t, build(t, manyDocs(6), 0))
	rows, err := ix.docRows()
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].Address = ""
	}
	hits, err := search(ix, "the", 0)
	if err != nil || len(hits) != 6 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
	for _, h := range hits {
		if h.Address == "" {
			t.Errorf("hit %d lost its address", h.Doc)
		}
	}
}

// Verify checks the address in a row against the fragment it names.
func TestVerifyCatchesARowWhoseAddressIsNotItsFragments(t *testing.T) {
	dir := build(t, manyDocs(4), 0)
	if err := Verify(dir); err != nil {
		t.Fatal(err)
	}
	ix := open(t, dir)
	rows, _ := ix.docRows()
	rows[2].Address = "https://elsewhere.example/other.md"
	err := verifyIndex(ix)
	if err == nil || !IsDataError(err) || !strings.Contains(err.Error(), "elsewhere.example") {
		t.Errorf("verify: %v", err)
	}
}

func rowBytes(t *testing.T, elems ...any) []byte {
	t.Helper()
	b, err := encMode.Marshal(elems)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDocumentRowDecoding(t *testing.T) {
	digest := make([]byte, 32)
	var r documentRow
	if err := r.UnmarshalCBOR(rowBytes(t, uint64(5), digest)); err != nil || r.WordCount != 5 || r.Address != "" {
		t.Errorf("two elements: %+v %v", r, err)
	}
	r = documentRow{}
	if err := r.UnmarshalCBOR(rowBytes(t, uint64(5), digest, "https://example.org/a.md")); err != nil || r.Address != "https://example.org/a.md" {
		t.Errorf("three elements: %+v %v", r, err)
	}
	r = documentRow{}
	if err := r.UnmarshalCBOR(rowBytes(t, uint64(5), digest, "https://example.org/a.md", "later", uint64(9))); err != nil || r.Address != "https://example.org/a.md" {
		t.Errorf("unknown trailing elements: %+v %v", r, err)
	}
	if err := r.UnmarshalCBOR(rowBytes(t, uint64(5), digest, uint64(7))); !IsDataError(err) {
		t.Errorf("an address that is not text: %v", err)
	}
	if err := r.UnmarshalCBOR(rowBytes(t, uint64(5))); !IsDataError(err) {
		t.Errorf("one element: %v", err)
	}
}

// A document table is cut by size as well as by count, so long addresses cannot
// make a table too big for a file.
func TestDocumentTablesAreCutBeforeLongAddressesFillAFile(t *testing.T) {
	var docs []Doc
	long := strings.Repeat("x", 900)
	for i := 0; i < 300; i++ {
		docs = append(docs, Doc{Address: "https://example.org/" + long + "/" + fmt.Sprintf("%05d", i) + ".md", Body: "word"})
	}
	dir := t.TempDir()
	// 300 rows of about 1 KB would be 300 KB in one table; the limit allows 64 KB
	if err := Build(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, FileLimit: 64 << 10}); err != nil {
		t.Fatalf("long addresses stopped the build: %v", err)
	}
	m := manifestOf(t, dir)
	if len(m.Documents.Ranges) < 5 {
		t.Errorf("%d tables for 300 rows of 1 KB under a 64 KB limit", len(m.Documents.Ranges))
	}
	next := uint64(0)
	for _, r := range m.Documents.Ranges {
		if r.First != next || r.Last < r.First {
			t.Fatalf("ranges are not contiguous at %+v (want first %d)", r, next)
		}
		next = r.Last + 1
	}
	if next != 300 {
		t.Errorf("ranges cover %d documents, want 300", next)
	}
	if err := Verify(dir); err != nil {
		t.Error(err)
	}
	ix := open(t, dir)
	hits, err := search(ix, "word", 0)
	if err != nil || len(hits) != 300 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
}
