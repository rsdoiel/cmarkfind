package corpus

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Phase 6b through the corpus: a set's mapping decides what is built, a query's
// field clauses are answered from the front matter, and an aggregate search
// reads each set by its own mapping.

var authorPages = map[string]string{
	"d1.md": "---\ntitle: Oberon notes\nauthor: R. S. Doiel\nkeywords: [Go, Oberon]\n---\ncompilers and runtimes\n",
	"d2.md": "---\ntitle: Other notes\nauthor: Ann Example\nkeywords: \"go, wasm\"\n---\ncompilers and gardens\n",
	"d3.md": "# Plain\n\ngardens only, author unknown\n",
}

func hits(t *testing.T, db *DB, q string, opts SearchOptions) []string {
	t.Helper()
	opts.Limit = 50
	r, err := db.SearchWith(context.Background(), q, opts)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.Address[strings.LastIndex(h.Address, "/")+1:])
	}
	sort.Strings(out)
	return out
}

func TestFieldClausesFindPagesByTheirFrontMatterThroughTheCorpus(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, hostFolder(t, "a.example", authorPages))
	for q, want := range map[string][]string{
		"author:doiel":                  {"d1.md"},
		`author:"ann example"`:          {"d2.md"},
		"keywords:go":                   {"d1.md", "d2.md"}, // a list, and a comma string
		"keywords:wasm":                 {"d2.md"},
		"keywords:oberon AND compilers": {"d1.md"},
		"author:ann OR author:doiel":    {"d1.md", "d2.md"},
		"NOT author:ann":                {"d1.md", "d3.md"},
		"author:nobody":                 nil,
	} {
		if got := hits(t, db, q, SearchOptions{}); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
	// the clause is case-insensitive in its field name too
	if got := hits(t, db, "Author:DOIEL", SearchOptions{}); !reflect.DeepEqual(got, []string{"d1.md"}) {
		t.Errorf("Author:DOIEL: %v", got)
	}
}

// A field no set maps is text: plain:gardens is the words plain and gardens,
// which page d3 has (its title and its body).
func TestAFieldNoSetMapsIsReadAsText(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, hostFolder(t, "a.example", authorPages))
	if got := hits(t, db, "plain:gardens", SearchOptions{}); !reflect.DeepEqual(got, []string{"d3.md"}) {
		t.Errorf("plain:gardens: %v", got)
	}
}

// A field mapped in one set and not in another matches nothing in the second;
// the first is still searched. With `author=none` the second set reads it as text.
func TestAFieldMappedInOneSetMatchesNothingInAnotherAndIsNotAnError(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", authorPages)
	b := hostFolder(t, "b.example", map[string]string{
		"e1.md": "---\nauthor: Ann Elsewhere\n---\nsomething about ann and compilers\n",
	})
	fill(t, db, a)
	fill(t, db, b)
	if err := db.AssignFields(ctx, b, []string{"author=none"}); err != nil {
		t.Fatal(err)
	}
	db.BuildSets(ctx, "", 0)
	if got := hits(t, db, "author:ann", SearchOptions{}); !reflect.DeepEqual(got, []string{"d2.md"}) {
		t.Errorf("author:ann over both sets: %v (b does not map author, so it has no match)", got)
	}
	if got := hits(t, db, "author:ann", SearchOptions{Set: b}); len(got) != 0 {
		t.Errorf("author:ann in the set that does not map it: %v", got)
	}
	if got := hits(t, db, "author:ann", SearchOptions{Set: a}); !reflect.DeepEqual(got, []string{"d2.md"}) {
		t.Errorf("author:ann in the set that maps it: %v", got)
	}
	// nobody maps it: text, so the words author and ann both are needed
	db.AssignFields(ctx, a, []string{"author=none"})
	db.BuildSets(ctx, "", 0)
	if got := hits(t, db, "author:ann", SearchOptions{}); len(got) != 0 {
		t.Errorf("author:ann read as text over pages that have no word author: %v", got)
	}
}

func TestChangingTheMappingChangesWhatAFieldClauseFinds(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", map[string]string{"t.md": "---\ntags: [alpha, beta]\n---\nbody words\n"})
	fill(t, db, a)
	if got := hits(t, db, "tags:alpha", SearchOptions{}); len(got) != 0 {
		t.Fatalf("tags is unmapped by default: %v", got)
	}
	db.AssignFields(ctx, a, []string{"tags=facet,fielded"})
	if _, err := db.BuildSets(ctx, "", 0); err != nil {
		t.Fatal(err)
	}
	if got := hits(t, db, "tags:alpha", SearchOptions{}); !reflect.DeepEqual(got, []string{"t.md"}) {
		t.Errorf("tags:alpha after mapping: %v", got)
	}
}

// The text role puts a field's words into the description segment, so a plain
// search finds the page by them.
func TestTheTextRoleMakesAFieldsWordsPartOfThePlainSearch(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", authorPages)
	fill(t, db, a)
	if got := hits(t, db, "doiel", SearchOptions{}); len(got) != 0 {
		t.Fatalf("author is not searched text by default: %v", got)
	}
	db.AssignFields(ctx, a, []string{"author=facet,fielded,text"})
	db.BuildSets(ctx, "", 0)
	if got := hits(t, db, "doiel", SearchOptions{}); !reflect.DeepEqual(got, []string{"d1.md"}) {
		t.Errorf("a plain search for doiel: %v", got)
	}
	if got := hits(t, db, "description:doiel", SearchOptions{}); !reflect.DeepEqual(got, []string{"d1.md"}) {
		t.Errorf("description:doiel: %v", got)
	}
	if got := hits(t, db, "title:doiel", SearchOptions{}); len(got) != 0 {
		t.Errorf("the words are in the description segment only: %v", got)
	}
}

// keywords and tags given as one comma string are several values; any other
// field's string is one value.
func TestAKeywordsCommaStringIsSeveralValuesAndAnAuthorStringIsOne(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, hostFolder(t, "a.example", map[string]string{
		"k.md": "---\nkeywords: \"static sites, markdown ,go\"\nauthor: \"Doiel, R. S.\"\n---\nbody\n",
	}))
	for q, want := range map[string]int{
		`keywords:"static sites"`: 1, "keywords:markdown": 1, "keywords:go": 1, "keywords:static AND keywords:go": 1, "keywords:nothing": 0,
		`author:"doiel, r. s."`: 1, "author:doiel": 1,
	} {
		if got := len(hits(t, db, q, SearchOptions{})); got != want {
			t.Errorf("%q: %d hits, want %d", q, got, want)
		}
	}
}

// A page whose meta is not read yet has no field values; it is found by its
// text and by no field clause.
func TestAPageWithNoMetaIsFoundByTextAndByNoFieldClause(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", authorPages)
	fill(t, db, a)
	db.sql.Exec(`UPDATE pages SET meta = '' WHERE address LIKE '%/d1.md'`)
	db.sql.Exec(`UPDATE sets SET input_digest = 'stale'`)
	db.BuildSets(ctx, "", 0)
	if got := hits(t, db, "author:doiel", SearchOptions{}); len(got) != 0 {
		t.Errorf("author:doiel: %v", got)
	}
	if got := hits(t, db, "compilers runtimes", SearchOptions{}); !reflect.DeepEqual(got, []string{"d1.md"}) {
		t.Errorf("text search: %v", got)
	}
}
