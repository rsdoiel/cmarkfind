package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bigDoc is a page whose fragment cannot fit the test's file limit.
func bigDoc(address string) Doc {
	var w []string
	for i := 0; i < 3000; i++ {
		w = append(w, fmt.Sprintf("bigword%04d", i))
	}
	return Doc{Address: address, Title: "Huge", Body: strings.Join(w, " ")}
}

const skipLimit = 8192 // the small fixture's files fit; bigDoc's fragment (33 KB) does not

func smallDocs() []Doc { return fixture() }

// A page whose text is too big for one fragment file is left out of the index,
// not fatal: the build succeeds, says which page and why, and the others are
// found where they are.
func TestAPageTooBigForAFragmentIsLeftOutAndTheBuildSucceeds(t *testing.T) {
	// "a-huge" sorts first, so every later page's number moves down by one.
	docs := append([]Doc{bigDoc("https://example.org/a-huge.md")}, smallDocs()...)
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, FileLimit: skipLimit})
	if err != nil {
		t.Fatalf("the build failed: %v", err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].Address != "https://example.org/a-huge.md" || rep.Skipped[0].Bytes <= skipLimit {
		t.Fatalf("skipped = %+v", rep.Skipped)
	}
	joined := strings.Join(rep.Warnings, "\n")
	if !strings.Contains(joined, "https://example.org/a-huge.md") || !strings.Contains(joined, "not in the index") {
		t.Errorf("warnings %q do not name the page and say it is not indexed", joined)
	}
	if err := Verify(dir); err != nil {
		t.Errorf("verify: %v", err)
	}
	ix := open(t, dir)
	if ix.Count() != 3 {
		t.Errorf("count = %d, want the three small pages", ix.Count())
	}
	hits, err := search(ix, "tomatoes", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(addrs(hits), " ")
	if !strings.Contains(got, "https://example.org/a.md") || !strings.Contains(got, "https://example.org/b.md") || len(hits) != 2 {
		t.Errorf("tomatoes found %q", got)
	}
	if hits, _ := search(ix, "bigword0007", 10); len(hits) != 0 {
		t.Errorf("a word only the skipped page has found %v", addrs(hits))
	}
	// the hit's fragment is its own page's (page numbers shifted correctly)
	hits, _ = search(ix, "fox", 10)
	if len(hits) != 1 || hits[0].Address != "https://example.org/c.md" {
		t.Fatalf("fox found %v", addrs(hits))
	}
	f, err := ix.Fragment(hits[0].Doc)
	if err != nil || f.Address != "https://example.org/c.md" {
		t.Errorf("fragment of the hit: %v %+v", err, f)
	}
}

// Leaving a page out is the same as never having it: the files are the same.
func TestTheIndexWithASkippedPageIsTheIndexWithoutIt(t *testing.T) {
	with, without := t.TempDir(), t.TempDir()
	docs := append([]Doc{bigDoc("https://example.org/a-huge.md")}, smallDocs()...)
	if _, err := BuildWithReport(with, docs, BuildOptions{Generation: 1, Built: fixedTime, FileLimit: skipLimit}); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildWithReport(without, smallDocs(), BuildOptions{Generation: 1, Built: fixedTime, FileLimit: skipLimit}); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(with, "manifest.json"))
	b, _ := os.ReadFile(filepath.Join(without, "manifest.json"))
	if string(a) != string(b) {
		t.Errorf("manifests differ:\n%s\n%s", a, b)
	}
}

func TestABuildWithNothingSkippedReportsNone(t *testing.T) {
	rep, err := BuildWithReport(t.TempDir(), smallDocs(), BuildOptions{Generation: 1, Built: fixedTime, FileLimit: skipLimit})
	if err != nil || len(rep.Skipped) != 0 {
		t.Errorf("%v %+v", err, rep.Skipped)
	}
}

// Only a fragment is let go. A word chunk or a document table over the limit
// still fails the build: no page is to blame there.
func TestAWordChunkOverTheLimitStillFailsTheBuild(t *testing.T) {
	docs := limitDocs()
	probe := t.TempDir()
	if err := Build(probe, docs, BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	biggest := int64(0)
	for _, c := range manifestOf(t, probe).Chunks {
		biggest = max(biggest, c.File.Size)
	}
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30, FileLimit: int(biggest) - 1})
	if err == nil {
		t.Fatal("a chunk over the limit was written")
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("pages were skipped for a chunk's fault: %+v", rep.Skipped)
	}
}
