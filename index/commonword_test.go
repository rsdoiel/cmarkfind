package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A word's postings are never split across chunks, so a word common enough to
// fill a file past the limit used to make the build fail. Since the document-only
// rule a word over the entry budget loses its positions and keeps its pages, so
// the same corpus builds and searches. (The pages are kept under the 8 MiB a
// page may have; four million occurrences make an entry over the 2 MiB budget;
// splitting a word across chunks is a later, format-changing record.)
func TestAWordTooCommonForOneFileIsIndexedDocumentOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 4 MB entry")
	}
	body := strings.Repeat("the ", 1_000_000)
	var docs []Doc
	for _, n := range []string{"a", "b", "c", "d"} {
		docs = append(docs, Doc{Address: "https://example.org/" + n + ".md", Title: "t", Body: body + " zebra " + n})
	}
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(rep.DocumentOnly) != 1 || rep.DocumentOnly[0].Word != "the" || rep.DocumentOnly[0].Pages != 4 || rep.DocumentOnly[0].Positions != 4_000_000 {
		t.Fatalf("report %+v", rep.DocumentOnly)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("verify: %v", err)
	}
	ix := open(t, dir)
	hits, err := search(ix, "the", 0)
	if err != nil || len(hits) != 4 {
		t.Fatalf("search the: %d hits, %v", len(hits), err)
	}
	hits, err = search(ix, "the zebra a", 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search the zebra a: %d hits, %v", len(hits), err)
	}
}

func TestAFailedBuildLeavesTheExistingIndexInPlace(t *testing.T) {
	dir := t.TempDir()
	if err := Build(dir, fixture(), BuildOptions{Generation: 1, Built: fixedTime}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildWithReport(dir, fixture(), BuildOptions{Generation: 2, Built: fixedTime, FileLimit: 64})
	if err == nil {
		t.Fatal("a build over the file limit succeeded")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if string(before) != string(after) {
		t.Fatal("a failed build replaced the manifest")
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("existing index after a failed build: %v", err)
	}
}
