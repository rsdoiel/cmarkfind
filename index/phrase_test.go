package index

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func usesAt(weight int, pos ...int) []use {
	var out []use
	for _, p := range pos {
		out = append(out, use{pos: p, weight: weight})
	}
	return out
}

func startsOf(sets []map[int][]use) []use { return phraseStarts(sets, 0) }

func TestPhraseStartsOnHandmadeUses(t *testing.T) {
	// "a b c": a at 3 and 10, b at 4 and 11, c at 5 and 20: only 3 is a start
	a := map[int][]use{0: usesAt(1, 3, 10)}
	b := map[int][]use{0: usesAt(1, 4, 11)}
	c := map[int][]use{0: usesAt(1, 5, 20)}
	got := startsOf([]map[int][]use{a, b, c})
	if len(got) != 1 || got[0].pos != 3 || got[0].weight != 1 {
		t.Errorf("starts %+v", got)
	}
	// the words in other fields: weights must agree
	b2 := map[int][]use{0: usesAt(4, 4)}
	if got := startsOf([]map[int][]use{a, b2}); len(got) != 0 {
		t.Errorf("different fields: %+v", got)
	}
	// uses not in position order (a prefix gathers several words): same answer
	ab := map[int][]use{0: append(usesAt(1, 11), usesAt(1, 4)...)}
	if got := startsOf([]map[int][]use{a, ab}); len(got) != 2 || got[0].pos != 3 || got[1].pos != 10 {
		t.Errorf("unsorted uses: %+v", got)
	}
	// a word with no place known (document-only) matches in its field anywhere
	unk := map[int][]use{0: {{pos: unknownPos, weight: 1}}}
	if got := startsOf([]map[int][]use{a, unk, c}); len(got) != 0 {
		// a at 3 or 10, c at 5 or 20: a at 3 needs c at 5 (offset 2), yes; offsets 0 and 2 fit
		if len(got) != 1 || got[0].pos != 3 {
			t.Errorf("document-only word between: %+v", got)
		}
	} else {
		t.Errorf("document-only word between: no start")
	}
	// only unknown words: the field is all that can be said
	if got := startsOf([]map[int][]use{unk, unk}); len(got) != 1 || got[0].pos != unknownPos {
		t.Errorf("all unknown: %+v", got)
	}
	// a word missing from the page: no match
	if got := startsOf([]map[int][]use{a, {}}); len(got) != 0 {
		t.Errorf("missing word: %+v", got)
	}
}

// A phrase of the commonest characters on a long page has hundreds of
// thousands of places to try; each must cost a lookup, not a scan of the page.
func TestAPhraseOfVeryCommonWordsIsNotQuadratic(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	common := []rune("の的に")
	var docs []Doc
	for i := 0; i < 3; i++ {
		var b strings.Builder
		for k := 0; k < 120_000; k++ {
			b.WriteRune(common[r.Intn(len(common))])
		}
		docs = append(docs, Doc{Address: fmt.Sprintf("https://example.org/%d.md", i), Body: b.String()})
	}
	dir := t.TempDir()
	if err := Build(dir, docs, BuildOptions{Generation: 1, Built: fixedTime}); err != nil {
		t.Fatal(err)
	}
	ix := open(t, dir)
	done := make(chan []string, 1)
	go func() {
		hits, err := search(ix, "の的のにの的", 5)
		if err != nil {
			done <- []string{err.Error()}
			return
		}
		done <- addrs(hits)
	}()
	select {
	case got := <-done:
		if len(got) == 0 {
			t.Errorf("no hit for a phrase that must occur in 360,000 random characters of three")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a phrase of six common characters took over 10 s")
	}
}
