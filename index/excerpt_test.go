package index

import (
	"fmt"
	"strings"
	"testing"
)

func anchorDoc() Doc {
	return Doc{
		Address:     "https://example.org/g.md",
		Title:       "Garden guide",
		Description: "Soil and seeds",
		Body:        "Intro words here.\n\n# Soil\n\nTomatoes like warm soil.\n\n## The *big* idea\n\nSeeds.\n\n## Notes\n\nOne.\n\n## Notes\n\nTwo.\n\n### `code` heading\n\nEnd.\n\n## !!!\n\nNone.\n",
	}
}

func TestFragmentCarriesHeadingAnchors(t *testing.T) {
	ix := open(t, build(t, []Doc{anchorDoc()}, 0))
	f, err := ix.Fragment(0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Anchor{
		{Slug: "soil", Heading: "Soil", Level: 1},
		{Slug: "the-big-idea", Heading: "The big idea", Level: 2},
		{Slug: "notes", Heading: "Notes", Level: 2},
		{Slug: "notes-1", Heading: "Notes", Level: 2},
		{Slug: "code-heading", Heading: "code heading", Level: 3},
	}
	if len(f.Anchors) != len(want) {
		t.Fatalf("anchors = %+v", f.Anchors)
	}
	words := strings.Fields(f.Text)
	for i, a := range f.Anchors {
		if a.Slug != want[i].Slug || a.Heading != want[i].Heading || a.Level != want[i].Level {
			t.Errorf("anchor %d = %+v, want %+v", i, a, want[i])
		}
		// the position indexes the heading's first word of the fragment text
		first := strings.Fields(a.Heading)[0]
		if a.Position < 0 || a.Position >= len(words) || words[a.Position] != first {
			t.Errorf("anchor %d position %d is %q, want %q", i, a.Position, wordAt(words, a.Position), first)
		}
	}
	// positions are in order
	for i := 1; i < len(f.Anchors); i++ {
		if f.Anchors[i].Position <= f.Anchors[i-1].Position {
			t.Errorf("anchors out of order: %+v", f.Anchors)
		}
	}
}

func wordAt(words []string, i int) string {
	if i < 0 || i >= len(words) {
		return "<out of range>"
	}
	return words[i]
}

func TestAnchorsDoNotChangeTheWordInvariant(t *testing.T) {
	// the anchors are extra: the text and word count are what they were
	ix := open(t, build(t, []Doc{anchorDoc()}, 0))
	f, _ := ix.Fragment(0)
	if len(strings.Fields(f.Text)) != f.WordCount {
		t.Fatalf("words %d, count %d", len(strings.Fields(f.Text)), f.WordCount)
	}
}

func TestBuildIsStillDeterministicWithAnchors(t *testing.T) {
	a := build(t, []Doc{anchorDoc()}, 0)
	b := build(t, []Doc{anchorDoc()}, 0)
	if !sameTree(t, a, b) {
		t.Fatal("two builds differ")
	}
}

// numbered makes a fragment of the words w0 w1 ... w(n-1).
func numbered(n int) Fragment {
	ws := make([]string, n)
	for i := range ws {
		ws[i] = fmt.Sprintf("w%d", i)
	}
	return Fragment{Text: strings.Join(ws, " "), WordCount: n}
}

func TestExcerptWindowHasTheHighestSummedWeight(t *testing.T) {
	f := numbered(200)
	// one weak mark early, two strong ones together late
	marks := []Mark{{Pos: 10, Weight: 1}, {Pos: 150, Weight: 2}, {Pos: 155, Weight: 2}}
	e := MakeExcerpt(f, marks, 30)
	if e.Start > 150 || e.End <= 155 {
		t.Fatalf("window [%d,%d) misses the heavy cluster", e.Start, e.End)
	}
	if e.End-e.Start != 30 {
		t.Fatalf("window is %d words", e.End-e.Start)
	}
	if strings.Contains(e.Text, "w10 ") {
		t.Fatalf("the weak early hit is in the window: %q", e.Text)
	}
}

func TestExcerptTitleWeightBeatsTwoBodyHits(t *testing.T) {
	f := numbered(300)
	marks := []Mark{{Pos: 2, Weight: 4}, {Pos: 200, Weight: 1}, {Pos: 205, Weight: 1}}
	e := MakeExcerpt(f, marks, 30)
	if e.Start != 0 {
		t.Fatalf("window starts at %d, want the title hit's window", e.Start)
	}
}

func TestExcerptNoMarksStartsAtTheTop(t *testing.T) {
	e := MakeExcerpt(numbered(100), nil, 10)
	if e.Start != 0 || e.End != 10 || strings.Contains(e.HTML, "<mark>") {
		t.Fatalf("got %+v", e)
	}
}

func TestExcerptShortPageIsWhole(t *testing.T) {
	e := MakeExcerpt(numbered(5), []Mark{{Pos: 3, Weight: 1}}, 30)
	if e.Start != 0 || e.End != 5 {
		t.Fatalf("got [%d,%d)", e.Start, e.End)
	}
	if e.Text != "w0 w1 w2 w3 w4" {
		t.Fatalf("text = %q", e.Text)
	}
}

func TestExcerptEmptyPage(t *testing.T) {
	e := MakeExcerpt(Fragment{}, []Mark{{Pos: 0, Weight: 1}}, 30)
	if e.Text != "" || e.HTML != "" || e.Start != 0 || e.End != 0 {
		t.Fatalf("got %+v", e)
	}
}

func TestExcerptWindowStopsAtTheEndOfThePage(t *testing.T) {
	e := MakeExcerpt(numbered(100), []Mark{{Pos: 99, Weight: 1}}, 30)
	if e.End != 100 || e.End-e.Start != 30 {
		t.Fatalf("got [%d,%d)", e.Start, e.End)
	}
}

func TestExcerptMarksHitsAndEscapes(t *testing.T) {
	// a hostile index file can put anything in a fragment: the excerpt is HTML
	f := Fragment{Text: `a<script> b&c "hit" d>e`, WordCount: 5}
	e := MakeExcerpt(f, []Mark{{Pos: 1, Weight: 1}, {Pos: 2, Weight: 1}}, 30)
	want := `a&lt;script&gt; <mark>b&amp;c</mark> <mark>&#34;hit&#34;</mark> d&gt;e`
	if e.HTML != want {
		t.Fatalf("html = %q\nwant   %q", e.HTML, want)
	}
	if strings.Contains(e.HTML, "<script>") {
		t.Fatal("unescaped markup")
	}
	if e.Text != `a<script> b&c "hit" d>e` {
		t.Fatalf("text = %q", e.Text)
	}
}

func TestExcerptMarkedWordsAreOnlyThoseInTheWindow(t *testing.T) {
	e := MakeExcerpt(numbered(100), []Mark{{Pos: 50, Weight: 1}}, 10)
	if strings.Count(e.HTML, "<mark>") != 1 || !strings.Contains(e.HTML, "<mark>w50</mark>") {
		t.Fatalf("html = %q", e.HTML)
	}
}

func TestExcerptNamesTheSectionOfItsWindow(t *testing.T) {
	f := numbered(100)
	f.Anchors = []Anchor{{Slug: "intro", Heading: "Intro", Position: 5, Level: 1}, {Slug: "later", Heading: "Later", Position: 60, Level: 2}}
	e := MakeExcerpt(f, []Mark{{Pos: 70, Weight: 1}}, 10)
	if e.Anchor != "later" {
		t.Fatalf("anchor = %q", e.Anchor)
	}
	e = MakeExcerpt(f, []Mark{{Pos: 2, Weight: 1}}, 10)
	if e.Anchor != "" {
		t.Fatalf("before the first heading the anchor is empty, got %q", e.Anchor)
	}
}

func marksOf(t *testing.T, ix *Index, q string, doc int) []Mark {
	t.Helper()
	n, err := ParseQuery(q, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ix.Marks(n, doc)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func docOf(t *testing.T, ix *Index, addr string) int {
	t.Helper()
	for i := 0; i < ix.Count(); i++ {
		f, err := ix.Fragment(i)
		if err != nil {
			t.Fatal(err)
		}
		if f.Address == addr {
			return i
		}
	}
	t.Fatalf("no %s", addr)
	return -1
}

func TestMarksAreTheWordsOfTheQuery(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	b := docOf(t, ix, "https://example.org/b.md")
	f, _ := ix.Fragment(b)
	words := strings.Fields(f.Text)
	ms := marksOf(t, ix, "soil", b)
	if len(ms) < 2 {
		t.Fatalf("marks = %+v", ms)
	}
	for _, m := range ms {
		if !strings.EqualFold(words[m.Pos], "soil") {
			t.Errorf("mark at %d is %q", m.Pos, words[m.Pos])
		}
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].Pos <= ms[i-1].Pos {
			t.Errorf("marks not sorted and unique: %+v", ms)
		}
	}
}

func TestMarksOfAPhraseAreItsWordsInOrder(t *testing.T) {
	docs := []Doc{{Address: "https://x.org/p.md", Title: "T", Body: "warm soil here. soil warm there. warm soil again"}}
	ix := open(t, build(t, docs, 0))
	f, _ := ix.Fragment(0)
	words := strings.Fields(f.Text)
	ms := marksOf(t, ix, `"warm soil"`, 0)
	if len(ms) != 4 {
		t.Fatalf("marks = %+v (words %v)", ms, words)
	}
	for _, m := range ms {
		w := strings.ToLower(words[m.Pos])
		if w != "warm" && w != "soil" {
			t.Errorf("mark %d is %q", m.Pos, w)
		}
	}
	// "soil warm" in the middle is not part of the phrase
	for _, m := range ms {
		if m.Pos >= 5 && m.Pos <= 6 { // T=0, warm soil here=1..3, soil warm there=4..6
			t.Errorf("a word of the reversed pair is marked: %+v", ms)
		}
	}
}

func TestMarksIgnoreNotAndPrefixMatchesWords(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	b := docOf(t, ix, "https://example.org/b.md")
	f, _ := ix.Fragment(b)
	words := strings.Fields(f.Text)
	for _, m := range marksOf(t, ix, "tomat* -clay", b) {
		if !strings.HasPrefix(strings.ToLower(words[m.Pos]), "tomat") {
			t.Errorf("mark %d is %q", m.Pos, words[m.Pos])
		}
	}
	if got := marksOf(t, ix, "tomat* -clay", b); len(got) == 0 {
		t.Fatal("no marks for a prefix")
	}
	if got := marksOf(t, ix, "-soil", b); len(got) != 0 {
		t.Fatalf("a NOT clause marks %+v", got)
	}
}

func TestMarksOfAFieldClauseAreInThatField(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	b := docOf(t, ix, "https://example.org/b.md")
	all := marksOf(t, ix, "soil", b)
	desc := marksOf(t, ix, "description:soil", b)
	if len(desc) != 1 || len(all) <= 1 {
		t.Fatalf("all %+v, description %+v", all, desc)
	}
	if desc[0].Weight != weightDescription {
		t.Fatalf("weight = %d", desc[0].Weight)
	}
}

func TestIndexExcerptEndToEnd(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	n, _ := ParseQuery("seeds", nil)
	hits, _ := ix.Search(n, 10)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	e, err := ix.Excerpt(n, hits[0].Doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "<mark>") || !strings.Contains(strings.ToLower(e.HTML), "<mark>seeds</mark>") {
		t.Fatalf("html = %q", e.HTML)
	}
}
