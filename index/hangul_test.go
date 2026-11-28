package index

import (
	"strings"
	"testing"
)

func parse1(t *testing.T, q string) *Node {
	t.Helper()
	n, err := ParseQuery(q, nil)
	if err != nil || n == nil {
		t.Fatalf("%q: %v %v", q, n, err)
	}
	return n
}

// A Hangul word of two or more syllables in a query is a prefix: the particle
// that follows a stem is part of the page's word (한국어를), so the stem alone
// would find nothing.
func TestAHangulTermOfTwoSyllablesOrMoreIsAPrefix(t *testing.T) {
	for _, q := range []string{"한국어", "서울", "한국어*", "title:한국어"} {
		if n := parse1(t, q); n.Kind != KindTerm || !n.Prefix {
			t.Errorf("%q: kind %v prefix %v, want a prefix term", q, n.Kind, n.Prefix)
		}
	}
}

func TestTheHangulPrefixRuleLeavesEverythingElseAlone(t *testing.T) {
	for _, q := range []string{
		"한",         // one syllable: too short to be a stem
		`"한국어"`,     // a quoted word means exactly what it says
		"tomatoes",  // another script
		"猫",         // Han is one word per character already
		"iPhone한국어", // a script change ends a word: a phrase, not a term
		"한국어-서울",    // a hyphen makes a phrase
		"한국어2024",   // not all Hangul
	} {
		n := parse1(t, q)
		if n.Prefix {
			t.Errorf("%q became a prefix", q)
		}
	}
}

func TestTheHangulPrefixAppliesInsideBooleanQueries(t *testing.T) {
	n := parse1(t, "한국어 서울 -공부 (한글 OR 어휘)")
	var terms []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		if x.Kind == KindTerm {
			terms = append(terms, x)
		}
		for _, k := range x.Kids {
			walk(k)
		}
	}
	walk(n)
	if len(terms) != 5 {
		t.Fatalf("%d terms in %s", len(terms), n)
	}
	for _, x := range terms {
		if !x.Prefix {
			t.Errorf("term %v is not a prefix in %s", x.Words, n)
		}
	}
}

func TestHangulStemSearchFindsTheWordWithItsParticle(t *testing.T) {
	docs := []Doc{
		{Address: "https://example.org/ko.md", Body: "한국어를 공부합니다. 서울에서 만나요."},
		{Address: "https://example.org/en.md", Body: "Nothing in Korean here."},
	}
	ix := open(t, build(t, docs, 0))
	for q, want := range map[string]int{
		"한국어":     1, // the stem finds 한국어를
		"공부":      1,
		"서울":      1,
		`"한국어"`:   0, // exact: the page's word is 한국어를
		`"한국어를"`:  1,
		"한":       0, // one syllable is not a stem
		"한국어를":    1,
		"한국어 -공부": 0,
	} {
		hits, err := search(ix, q, 10)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if len(hits) != want {
			t.Errorf("%s: %d hits %v, want %d", q, len(hits), addrs(hits), want)
		}
	}
	_ = strings.TrimSpace
}
