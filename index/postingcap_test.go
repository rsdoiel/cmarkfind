package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func theOnEveryPage(n int) []Doc {
	var docs []Doc
	for i := 0; i < n; i++ {
		docs = append(docs, Doc{Address: fmt.Sprintf("https://example.org/%d.md", i), Body: fmt.Sprintf("the page%d", i)})
	}
	return docs
}

// The reader refuses an array of more than a million elements, and a word's
// posting list is one array. A build that would write one says so, naming the
// word and its pages, instead of writing an index every search refuses.
func TestAWordOnMoreThanTheReadersCapOfPagesFailsTheBuildNamingIt(t *testing.T) {
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, theOnEveryPage(6), BuildOptions{Generation: 1, Built: fixedTime, postingCap: 5})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error %v is not ErrTooLarge", err)
	}
	for _, want := range []string{`"the"`, "6 pages", "5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q lacks %q", err, want)
		}
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("pages were skipped for a word's fault: %+v", rep.Skipped)
	}
	if _, serr := os.Stat(filepath.Join(dir, "manifest.json")); serr == nil {
		t.Error("a manifest was written")
	}
}

func TestAWordOnExactlyTheCapOfPagesBuilds(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildWithReport(dir, theOnEveryPage(5), BuildOptions{Generation: 1, Built: fixedTime, postingCap: 5}); err != nil {
		t.Fatalf("a word on exactly the cap failed: %v", err)
	}
	if err := Verify(dir); err != nil {
		t.Error(err)
	}
}

// The cap the builder holds is the one the reader decodes with: the largest
// list the reader takes is the largest the builder writes.
func TestTheBuildersCapIsTheReadersCap(t *testing.T) {
	if defaultPostingCap != maxFiles {
		t.Fatalf("builder cap %d, reader cap %d", defaultPostingCap, maxFiles)
	}
	e := entry{Word: "the", Postings: make([]posting, defaultPostingCap)}
	for i := range e.Postings {
		e.Postings[i] = posting{Delta: 1, Locations: []int64{4}}
	}
	raw, err := encMode.Marshal(wordChunk{Version: 1, Analyzer: Analyzer, Generation: 1, Entries: []entry{e}})
	if err != nil {
		t.Fatal(err)
	}
	var wc wordChunk
	if err := decMode.Unmarshal(raw, &wc); err != nil {
		t.Errorf("a list of exactly the builder's cap does not decode: %v", err)
	}
}
