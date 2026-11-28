package index

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Tests for verification against the page text (a phrase through a
// document-only word is checked on the fragment of each hit, and NOT never
// takes the complement of a superset).

// everyShape returns queries of every form over the vocabulary of stopDocs:
// terms, prefixes, fields, phrases with and without common words, AND, OR,
// NOT and nesting.
func everyShape(docs []Doc) []string {
	qs := []string{
		"the", "of", "w01", "w0*", "th*", "title:the", "body:of", "description:to", "title:w03",
		"the of", "the OR w05", "the -of", "-the", "-w01", "(the OR to) AND w12",
		`"the of"`, `"of the"`, `"to the"`, `"the w01"`, `"w01 the"`, `"w02 w03"`, `"the of to"`, `"and the of"`,
		`title:"the of"`, `body:"to the"`, `description:"of the"`, `"the o*"`, `"to th*"`, `"w0* the"`,
		`w01 -"of the"`, `-"of the"`, `-"the of"`, `w02 NOT "to the"`, `("of the" OR w04) -w05`,
		`"of the" OR "to the"`, `-("of the" OR "to the")`, `-(-"of the")`, `w01 -(w02 -"the of")`,
		`title:(the OR "of the")`, `-title:"of the"`, `"of the" -"to the"`, `the -"the of" -"of the"`,
	}
	for _, q := range phraseSamples(docs, 40) {
		qs = append(qs, q, "w05 -"+q, "-"+q)
	}
	return qs
}

func TestAQueryOverADocumentOnlyIndexFindsExactlyThePagesTheExactIndexFinds(t *testing.T) {
	docs := stopDocs(80)
	exact := open(t, build(t, docs, 0))
	for _, budget := range []int{smallBudget, 1000} {
		dir, rep := buildBudget(t, docs, budget)
		if len(rep.DocumentOnly) < 2 {
			t.Fatalf("budget %d: %d document-only words", budget, len(rep.DocumentOnly))
		}
		lean := open(t, dir)
		for _, q := range everyShape(docs) {
			want := pageSet(t, exact, q)
			if got := pageSet(t, lean, q); !slices.Equal(got, want) {
				t.Errorf("budget %d, %s: found %d pages, the exact index %d\n got  %v\n want %v", budget, q, len(got), len(want), got, want)
			}
			// with a limit: only true pages, and as many as there are, up to the limit
			hits, err := search(lean, q, 7)
			if err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if len(hits) != min(7, len(want)) {
				t.Errorf("budget %d, %s, limit 7: %d hits, want %d", budget, q, len(hits), min(7, len(want)))
			}
			for _, a := range addrs(hits) {
				if !slices.Contains(want, a) {
					t.Errorf("budget %d, %s, limit 7: %s is not an answer", budget, q, a)
				}
			}
		}
	}
}

func TestMatchPageAgreesWithTheIndexOnQueriesWithoutDocumentOnlyWords(t *testing.T) {
	docs := append(stopDocs(40), fixture()...)
	ix := open(t, build(t, docs, 0))
	for _, q := range append(everyShape(docs), semanticQueries...) {
		n, err := ParseQuery(q, nil)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := ix.Search(n, 0)
		if err != nil {
			t.Fatal(err)
		}
		var viaIndex, viaText []string
		for _, h := range hits {
			viaIndex = append(viaIndex, h.Address)
		}
		for d := 0; d < ix.Count(); d++ {
			fr, err := ix.fragment(d)
			if err != nil {
				t.Fatal(err)
			}
			if matchPage(n, fr) {
				viaText = append(viaText, fr.Address)
			}
		}
		if !slices.Equal(sortedCopy(viaIndex), sortedCopy(viaText)) {
			t.Errorf("%s: the index finds %d pages, the page text %d", q, len(viaIndex), len(viaText))
		}
	}
}

// openCounting opens dir through a Source that counts how many fragment files
// are opened, by name.
type countingFS struct {
	fs.FS
	mu     sync.Mutex
	opened map[string]int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, "f/") {
		c.mu.Lock()
		c.opened[name]++
		c.mu.Unlock()
	}
	return c.FS.Open(name)
}

func (c *countingFS) fragments() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.opened)
}

func TestVerificationStopsAtTheLimit(t *testing.T) {
	// 60 pages hold "to the" for real; "to" and "the" are document-only
	var docs []Doc
	for i := 0; i < 60; i++ {
		docs = append(docs, Doc{Address: fmt.Sprintf("https://example.org/%02d.md", i),
			Body: fmt.Sprintf("p%02d to the end ", i) + strings.Repeat("the ", 50+i) + strings.Repeat("to ", 50)})
	}
	dir, rep := buildBudget(t, docs, 400)
	if len(rep.DocumentOnly) < 2 {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	for _, c := range []struct {
		q     string
		limit int
		max   int
	}{
		{`"to the"`, 5, 5},     // every candidate is true: five fragments
		{`"to the"`, 0, 60},    // no limit: all of them
		{"the", 5, 5},          // no phrase: nothing to verify, the five shown
		{`"to the" p07`, 5, 1}, // one page
	} {
		cf := &countingFS{FS: os.DirFS(dir), opened: map[string]int{}}
		ix, err := OpenSource(cf)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := search(ix, c.q, c.limit)
		if err != nil {
			t.Fatal(err)
		}
		if got := cf.fragments(); got > c.max {
			t.Errorf("%s, limit %d: %d fragments opened, want at most %d (%d hits)", c.q, c.limit, got, c.max, len(hits))
		}
		if c.limit > 0 && len(hits) > c.limit {
			t.Errorf("%s: %d hits over the limit %d", c.q, len(hits), c.limit)
		}
	}
}

func TestAQueryWithNoDocumentOnlyWordOpensOnlyTheFragmentsItShows(t *testing.T) {
	docs := stopDocs(60)
	dir := build(t, docs, 0)
	cf := &countingFS{FS: os.DirFS(dir), opened: map[string]int{}}
	ix, err := OpenSource(cf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search(ix, `"w01 w02" OR w03`, 5); err != nil {
		t.Fatal(err)
	}
	if got := cf.fragments(); got > 5 {
		t.Errorf("%d fragments opened for five hits", got)
	}
}

func TestMarksOfADocumentOnlyWordAndOfAPhraseThroughOneComeFromThePageText(t *testing.T) {
	// words: 0 a, 1 of, 2 the, 3 b, 4 of, 5 c, 6 the, then filler that makes "of" and "the" document-only
	body := "a of the b of c the " + strings.Repeat("of the ", 300)
	docs := []Doc{{Address: "https://example.org/1.md", Body: body}}
	dir, rep := buildBudget(t, docs, 200)
	if len(rep.DocumentOnly) != 2 {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	ix := open(t, dir)
	positions := func(q string) []int {
		marks, err := ix.Marks(mustParse(t, q), 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, m := range marks {
			out = append(out, m.Pos)
		}
		return out
	}
	if got := positions(`"of the"`); len(got) < 4 || !slices.Equal(got[:4], []int{1, 2, 7, 8}) {
		t.Errorf(`marks of "of the": %v`, got)
	}
	if slices.Contains(positions(`"of the"`), 4) || slices.Contains(positions(`"of the"`), 6) {
		t.Error(`"of the" marked the 'of' at 4 or the 'the' at 6, which are not next to each other`)
	}
	if got := positions(`"c the"`); !slices.Equal(got, []int{5, 6}) {
		t.Errorf(`marks of "c the": %v`, got)
	}
	ex, err := ix.Excerpt(mustParse(t, `"of the"`), 0, 10)
	if err != nil || !strings.Contains(ex.HTML, "<mark>of</mark>") || !strings.Contains(ex.HTML, "<mark>the</mark>") {
		t.Errorf("excerpt %q, %v", ex.HTML, err)
	}
}

func TestNotOfAnExactClauseIsStillTheComplement(t *testing.T) {
	docs := stopDocs(40)
	exact := open(t, build(t, docs, 0))
	dir, _ := buildBudget(t, docs, smallBudget)
	lean := open(t, dir)
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 20; i++ {
		q := fmt.Sprintf("w%02d -w%02d", r.Intn(40), r.Intn(40))
		if got, want := pageSet(t, lean, q), pageSet(t, exact, q); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", q, got, want)
		}
	}
}
