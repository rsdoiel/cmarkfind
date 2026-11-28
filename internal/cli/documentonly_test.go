package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bigSite is a site with four pages of 600,000 "the" each (together over the
// 2 MiB entry budget; a page over 8 MiB is refused by the fill) and one small page.
func bigSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range []string{"big1.md", "big2.md", "big3.md", "big4.md"} {
		os.WriteFile(filepath.Join(root, n), []byte("# Big\n\n"+strings.Repeat("the ", 600_000)+"\n\nzebra stripes\n"), 0o644)
	}
	os.WriteFile(filepath.Join(root, "small.md"), []byte("# Small\n\nThe zebra and the otter.\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(fmt.Sprintf(
		`<opml version="2.0"><head/><body><outline text="big1" url="https://example.org/big1.md"/><outline text="big2" url="https://example.org/big2.md"/><outline text="big3" url="https://example.org/big3.md"/><outline text="big4" url="https://example.org/big4.md"/><outline text="small" url="https://example.org/small.md"/></body></opml>`)), 0o644)
	return root
}

func TestAFillWarnsOnceThatAWordIsNowIndexedWithoutPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	db := dbPath(t)
	root := bigSite(t)
	r := do(t, "-db", db, "fill", root)
	if r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	if !strings.HasPrefix(r.errOut, "cmarkfind: warning: ") || !strings.Contains(r.errOut, "without positions") || !strings.Contains(r.errOut, "the") {
		t.Errorf("stderr %q", r.errOut)
	}
	if strings.Contains(r.out, "without positions") {
		t.Errorf("a warning on standard output: %q", r.out)
	}
	// a second fill that changes a page: same set, no warning
	os.WriteFile(filepath.Join(root, "small.md"), []byte("# Small\n\nThe zebra and the badger.\n"), 0o644)
	r = do(t, "-db", db, "fill", root)
	if r.code != 0 || strings.Contains(r.errOut, "without positions") {
		t.Errorf("second fill: %+v", r)
	}
}

func TestStatsListsTheDocumentOnlyWords(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", bigSite(t)); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	r := do(t, "-db", db, "stats")
	if r.code != 0 || !strings.Contains(r.out, "document-only 1") || !strings.Contains(r.out, "the") {
		t.Errorf("stats: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "stats", "-json").out)
	words, _ := j["document_only"].([]any)
	if len(words) != 1 || words[0] != "the" {
		t.Errorf("json document_only %v", j["document_only"])
	}
}

func TestIndexerBuildAndListNameTheDocumentOnlyWords(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", bigSite(t)); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	dir := filepath.Join(t.TempDir(), "idx")
	r := do(t, "-db", db, "indexer", "build", dir)
	if r.code != 0 || !strings.Contains(r.out, "document-only") || !strings.Contains(r.out, "the") {
		t.Errorf("build: %+v", r)
	}
	r = do(t, "-db", db, "indexer", "list", dir)
	if r.code != 0 || !strings.Contains(r.out, "document-only") || !strings.Contains(r.out, "the") {
		t.Errorf("list: %+v", r)
	}
	// and the index search path works on it
	r = do(t, "-db", db, "search", "-index", dir, "the zebra")
	if r.code != 0 || !strings.Contains(r.out, "small.md") || !strings.Contains(r.out, "big1.md") {
		t.Errorf("search: %+v", r)
	}
}

func TestAnIndexWithNoDocumentOnlyWordPrintsNoSuchLine(t *testing.T) {
	_, dir := built(t)
	if r := do(t, "indexer", "list", dir); strings.Contains(r.out, "document-only") {
		t.Errorf("list: %q", r.out)
	}
}

func TestSearchThroughADocumentOnlyWordIsExact(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	db := dbPath(t)
	root := bigSite(t)
	os.WriteFile(filepath.Join(root, "apart.md"), []byte("# Apart\n\nA zebra stripes, and then the otter.\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(
		`<opml version="2.0"><head/><body><outline text="big1" url="https://example.org/big1.md"/><outline text="big2" url="https://example.org/big2.md"/><outline text="big3" url="https://example.org/big3.md"/><outline text="big4" url="https://example.org/big4.md"/><outline text="small" url="https://example.org/small.md"/><outline text="apart" url="https://example.org/apart.md"/></body></opml>`), 0o644)
	if r := do(t, "-db", db, "fill", root); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	r := do(t, "-db", db, "search", "-limit", "20", `"the zebra"`)
	if r.code != 0 || strings.Contains(r.out, "apart.md") || !strings.Contains(r.out, "small.md") || !strings.Contains(r.out, "big1.md") {
		t.Errorf("phrase: %+v", r)
	}
	r = do(t, "-db", db, "search", "-limit", "20", `NOT "the zebra"`)
	if r.code != 0 || !strings.Contains(r.out, "apart.md") || strings.Contains(r.out, "small.md") {
		t.Errorf("not phrase: %+v", r)
	}
}
