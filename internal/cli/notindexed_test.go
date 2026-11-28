package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hugeSite is a site with one page of 2.2 million CJK characters (a fragment of
// about 8.8 MB, over the 8 MiB file limit) and one small page.
func hugeSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("# 長い本\n\n")
	x := uint32(1)
	for i := 0; i < 2_200_000; i++ {
		x = x*1664525 + 1013904223
		b.WriteRune(rune(0x4e00 + (x>>8)%3000))
	}
	b.WriteString("\n")
	os.WriteFile(filepath.Join(root, "huge.md"), []byte(b.String()), 0o644)
	os.WriteFile(filepath.Join(root, "small.md"), []byte("# Small\n\nbadgers dig setts\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(`<?xml version="1.0"?><opml version="2.0"><head/><body>`+
		`<outline text="s" url="https://example.org/small.md"/><outline text="h" url="https://example.org/huge.md"/></body></opml>`), 0o644)
	return root
}

// One page too big for the index must not stop the corpus being searched: the
// fill succeeds and warns, the other page is found, stats says what is missing.
func TestAPageTooBigForTheIndexIsWarnedAboutAndTheCorpusStaysSearchable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an index over a 2.2 million character page")
	}
	db, site := dbPath(t), hugeSite(t)
	r := do(t, "-db", db, "fill", site)
	if r.code != 0 || !strings.Contains(r.out, "added 2") {
		t.Fatalf("fill: %+v", r)
	}
	if !strings.Contains(r.errOut, "warning:") || !strings.Contains(r.errOut, "https://example.org/huge.md is not in the index") {
		t.Errorf("fill's warning: %q", r.errOut)
	}
	r = do(t, "-db", db, "search", "badgers")
	if r.code != 0 || !strings.Contains(r.out, "https://example.org/small.md") {
		t.Fatalf("search: %+v", r)
	}
	r = do(t, "-db", db, "stats")
	if r.code != 0 || !strings.Contains(r.out, "not indexed 1  https://example.org/huge.md") {
		t.Errorf("stats text: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "stats", "-json").out)
	ni, ok := j["not_indexed"].([]any)
	if !ok || len(ni) != 1 || ni[0] != "https://example.org/huge.md" {
		t.Errorf("stats json not_indexed = %v", j["not_indexed"])
	}
	// Excluding the page rebuilds the index without it; nothing is missing now.
	if r := do(t, "-db", db, "exclude", "https://example.org/huge.md"); r.code != 0 {
		t.Fatalf("exclude: %+v", r)
	}
	r = do(t, "-db", db, "stats")
	if strings.Contains(r.out, "not indexed") {
		t.Errorf("stats after the page is gone: %q", r.out)
	}
	j = parseJSON(t, do(t, "-db", db, "stats", "-json").out)
	if ni, ok := j["not_indexed"].([]any); !ok || len(ni) != 0 {
		t.Errorf("not_indexed must be [] when nothing is missing, got %v", j["not_indexed"])
	}
}

// A corpus with nothing missing says nothing about it.
func TestStatsOfACorpusWithNothingMissingHasNoNotIndexedLine(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "stats")
	if strings.Contains(r.out, "not indexed") || r.errOut != "" {
		t.Errorf("%+v", r)
	}
	if j := parseJSON(t, do(t, "-db", db, "stats", "-json").out); fmt.Sprint(j["not_indexed"]) != "[]" {
		t.Errorf("not_indexed = %v", j["not_indexed"])
	}
}
