package cli

import (
	"strings"
	"testing"
)

// Phase 6b through the command: author:doiel, keywords:go and the mapping that
// decides them, without any indexer build in between.

var fieldPagesCLI = map[string]string{
	"d1.md": "---\ntitle: Oberon notes\nauthor: R. S. Doiel\nkeywords: [Go, Oberon]\ntags: [alpha]\n---\ncompilers and runtimes\n",
	"d2.md": "---\ntitle: Other notes\nauthor: Ann Example\nkeywords: \"go, wasm\"\n---\ncompilers and gardens\n",
	"d3.md": "# Plain\n\ngardens only\n",
}

func fieldSet(t *testing.T) (db, set string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	root := hostSite(t, "f.example", fieldPagesCLI)
	db = dbPath(t)
	if r := do(t, "-db", db, "fill", root); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	return db, root
}

func addrs(t *testing.T, db string, args ...string) []string {
	t.Helper()
	r := do(t, append([]string{"-db", db, args[0], "-json", "-limit", "20"}, args[1:]...)...)
	if r.code != 0 && r.code != 1 {
		t.Fatalf("%v: %+v", args, r)
	}
	var out []string
	if r.out == "" {
		return nil
	}
	hs, _ := parseJSON(t, r.out)["hits"].([]any)
	for _, h := range hs {
		a := h.(map[string]any)["address"].(string)
		out = append(out, a[strings.LastIndex(a, "/")+1:])
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestSearchAnswersFieldClausesFromTheFrontMatter(t *testing.T) {
	db, _ := fieldSet(t)
	for q, want := range map[string][]string{
		"author:doiel":                {"d1.md"},
		`author:"ann example"`:        {"d2.md"},
		"keywords:go":                 {"d1.md", "d2.md"},
		"keywords:go AND compilers":   {"d1.md", "d2.md"},
		"keywords:oberon -author:ann": {"d1.md"},
		"author:nobody":               nil,
	} {
		if got := addrs(t, db, "search", q); !same(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
	if r := do(t, "-db", db, "search", "author:doiel"); r.code != 0 || !strings.Contains(r.out, "Oberon notes") {
		t.Errorf("the plain output: %+v", r)
	}
	if r := do(t, "-db", db, "search", "author:nobody"); r.code != 1 {
		t.Errorf("no match: %+v", r)
	}
}

func TestSetFieldsDecidesWhatAFieldClauseMeansWithoutAnIndexerBuild(t *testing.T) {
	db, set := fieldSet(t)
	if got := addrs(t, db, "search", "tags:alpha"); len(got) != 0 {
		t.Fatalf("tags is unmapped by default: %v", got)
	}
	if r := do(t, "-db", db, "set", "fields", set, "tags=facet,fielded"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := addrs(t, db, "search", "tags:alpha"); !same(got, []string{"d1.md"}) {
		t.Errorf("tags:alpha after mapping: %v", got)
	}
	if r := do(t, "-db", db, "set", "fields", set, "author=none"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := addrs(t, db, "search", "author:doiel"); len(got) != 0 { // read as the words author and doiel
		t.Errorf("author:doiel after author=none: %v", got)
	}
	if r := do(t, "-db", db, "set", "fields", "-reset", set); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := addrs(t, db, "search", "author:doiel"); !same(got, []string{"d1.md"}) {
		t.Errorf("author:doiel after reset: %v", got)
	}
	if got := addrs(t, db, "search", "tags:alpha"); len(got) != 0 {
		t.Errorf("tags:alpha after reset: %v", got)
	}
}

func TestTheTextRoleLetsAPlainSearchFindAnAuthor(t *testing.T) {
	db, set := fieldSet(t)
	if got := addrs(t, db, "search", "doiel"); len(got) != 0 {
		t.Fatalf("author is not searched text by default: %v", got)
	}
	do(t, "-db", db, "set", "fields", set, "author=facet,fielded,text")
	if got := addrs(t, db, "search", "doiel"); !same(got, []string{"d1.md"}) {
		t.Errorf("plain search for doiel: %v", got)
	}
}

func TestAFieldMappedInOneSetIsSearchedThereAndMatchesNothingInTheOther(t *testing.T) {
	db, a := fieldSet(t)
	b := hostSite(t, "g.example", map[string]string{"e1.md": "---\nauthor: Ann Elsewhere\n---\nann and compilers\n"})
	if r := do(t, "-db", db, "fill", b); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	do(t, "-db", db, "set", "fields", b, "author=none")
	if got := addrs(t, db, "search", "author:ann"); !same(got, []string{"d2.md"}) {
		t.Errorf("both sets: %v", got)
	}
	if got := addrs(t, db, "search-set", b, "author:ann"); len(got) != 0 {
		t.Errorf("the set that does not map author: %v", got)
	}
	if got := addrs(t, db, "search-set", a, "author:ann"); !same(got, []string{"d2.md"}) {
		t.Errorf("the set that maps it: %v", got)
	}
}

func TestVerifyAndValidateCoverFilterFiles(t *testing.T) {
	db, set := fieldSet(t)
	if r := do(t, "-db", db, "indexer", "validate", set); r.code != 0 {
		t.Fatalf("%+v", r)
	}
}
