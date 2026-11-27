package oracle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/markdownweb/toc"
)

// fixturePages are what the harness is tested on. Each page exists for a case:
//
//	a.md  the word "otter" in title, description and body
//	b.md  "otter" only inside a link target (markup the index strips)
//	c.md  a phrase and a prefix
//	d.md  a page that says nothing the queries ask for
var fixturePages = map[string]string{
	"a.md": "---\ntitle: Otters of the river\ndescription: Notes on otter habits\n---\n\n# Otters\n\nThe otter swims in shallow water.\n",
	"b.md": "# Links\n\nSee [the guide](https://example.org/otter-guide) for more.\n",
	"c.md": "# Water\n\nShallow water holds the crane. Cranes wade; cranberries do not.\n",
	"d.md": "# Nothing\n\nA page about gardens.\n",
}

// openFixture fills a real corpus file from fixturePages and returns it with
// its path.
func openFixture(t *testing.T) (*corpus.DB, string) {
	t.Helper()
	root := t.TempDir()
	var lines strings.Builder
	var names []string
	for n := range fixturePages {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&lines, `<outline text="%s" url="https://example.org/%s"/>`, n, n)
		if err := os.WriteFile(filepath.Join(root, n), []byte(fixturePages[n]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, ".well-known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body>` + lines.String() + `</body></opml>`
	if err := os.WriteFile(filepath.Join(dir, "toc.opml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "corpus.db")
	db, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.FillLocal(context.Background(), root, toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func row(t *testing.T, r *Report, name string) Row {
	t.Helper()
	for _, x := range r.Rows {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no row %q in %+v", name, r.Rows)
	return Row{}
}

func run(t *testing.T, qs []Query) *Report {
	t.Helper()
	db, path := openFixture(t)
	r, err := Run(context.Background(), db, qs, Options{K: 10, Repeats: 3, DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A word both engines see the same way: the sets agree, the index agrees with
// the brute-force scan, and nothing is left to explain.
func TestAgreementOnAPlainWord(t *testing.T) {
	r := run(t, []Query{{Name: "water", FTS: "water", Index: "water"}})
	w := row(t, r, "water")
	if w.IndexVsBrute != 0 {
		t.Errorf("index and brute force disagree on %d pages", w.IndexVsBrute)
	}
	if len(w.OnlyFTS) != 0 || len(w.OnlyIndex) != 0 {
		t.Errorf("sets differ: only FTS %v, only index %v", w.OnlyFTS, w.OnlyIndex)
	}
	if w.FTSCount != 2 || w.IndexCount != 2 {
		t.Errorf("counts FTS %d index %d, want 2 and 2", w.FTSCount, w.IndexCount)
	}
	if w.Overlap != 1 {
		t.Errorf("overlap %v, want 1", w.Overlap)
	}
}

// FTS5 indexes the Markdown as written, so a word that is only in a link target
// matches there and not in the index. The harness says that is the cause.
func TestLinkTargetDifferenceIsExplained(t *testing.T) {
	r := run(t, []Query{{Name: "otter", FTS: "otter", Index: "otter"}})
	w := row(t, r, "otter")
	if got := w.OnlyFTS; len(got) != 1 || got[0] != "https://example.org/b.md" {
		t.Fatalf("only FTS = %v, want just b.md", got)
	}
	if len(w.OnlyIndex) != 0 {
		t.Errorf("only index = %v", w.OnlyIndex)
	}
	if w.Explained != 1 || w.Unexplained != 0 {
		t.Errorf("explained %d unexplained %d, want 1 and 0", w.Explained, w.Unexplained)
	}
	if w.IndexVsBrute != 0 {
		t.Errorf("index and brute force disagree on %d pages", w.IndexVsBrute)
	}
}

// A difference nothing about markup explains is reported as unexplained, so a
// real bug cannot hide in the explained count.
func TestUnexplainedDifferenceIsCounted(t *testing.T) {
	// FTS5 is told the word "garden" (a prefix of a word on d.md); the index is
	// told "gardens", which matches the same page, and "crane" against "cranes":
	// FTS5 has no stemming either, so give them different questions instead.
	r := run(t, []Query{{Name: "split", FTS: "garden*", Index: "crane"}})
	w := row(t, r, "split")
	if w.Unexplained == 0 {
		t.Errorf("different questions must leave unexplained pages: %+v", w)
	}
}

// Prefix and phrase queries run on both engines.
func TestPrefixAndPhrase(t *testing.T) {
	r := run(t, []Query{
		{Name: "prefix", FTS: "cran*", Index: "cran*"},
		{Name: "phrase", FTS: `"shallow water"`, Index: `"shallow water"`},
	})
	for _, n := range []string{"prefix", "phrase"} {
		w := row(t, r, n)
		if len(w.OnlyFTS)+len(w.OnlyIndex) != 0 || w.IndexVsBrute != 0 {
			t.Errorf("%s: %+v", n, w)
		}
	}
	if p := row(t, r, "prefix"); p.IndexCount != 1 {
		t.Errorf("prefix found %d pages, want 1", p.IndexCount)
	}
}

// The report carries the cost side: pages, rebuild time, sizes, latencies.
func TestReportNumbers(t *testing.T) {
	r := run(t, []Query{{Name: "water", FTS: "water", Index: "water"}})
	if r.Pages != 4 {
		t.Errorf("pages %d, want 4", r.Pages)
	}
	if r.Rebuild <= 0 || r.IndexBytes <= 0 || r.CorpusBytes <= 0 {
		t.Errorf("costs not measured: %+v", r)
	}
	w := row(t, r, "water")
	if w.FTSTime <= 0 || w.IndexTime <= 0 {
		t.Errorf("latency not measured: %+v", w)
	}
}

// Brute is the oracle that outlives FTS5: it answers from the pages alone.
func TestBruteScan(t *testing.T) {
	pages := []corpus.IndexPage{
		{Address: "x", Title: "Otters", Body: "swim"},
		{Address: "y", Body: "A [link](https://e.org/otter)."},
	}
	n, err := index.ParseQuery("otters", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := Brute(pages, n); len(got) != 1 || got[0] != "x" {
		t.Errorf("brute = %v", got)
	}
	n, _ = index.ParseQuery("otter", nil)
	if got := Brute(pages, n); len(got) != 0 {
		t.Errorf("brute on stripped text found %v; the link target is markup", got)
	}
}

// The report writes as a Markdown table a person can paste into a hand-off.
func TestWriteMarkdown(t *testing.T) {
	r := run(t, []Query{{Name: "water", FTS: "water", Index: "water"}})
	var b bytes.Buffer
	r.Write(&b)
	out := b.String()
	for _, want := range []string{"| query |", "water", "rebuild", "overlap"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}
