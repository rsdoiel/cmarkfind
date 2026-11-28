package corpus

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Phase 6c through the corpus: date queries on each date field, and facets that
// a set opts into, counted over a result.

var datedPages = map[string]string{
	"p1.md": "---\ndatePublished: 2026-03-01\ndateCreated: 2025-01-10\nkeywords: [Go, Oberon]\nauthor: Ann\n---\ncompilers one\n",
	"p2.md": "---\ndatePublished: 2026-04-02\ndateCreated: 2026-04-01\nkeywords: [go]\nauthor: Ann\n---\ncompilers two\n",
	"p3.md": "---\ndatePublished: sometime\ndateCreated: 2025-12-31\nkeywords: \"go, wasm\"\nauthor: Bob\n---\ncompilers three\n",
	"p4.md": "compilers four, no front matter\n",
}

// facetAssignments is what a set says to facet on the publication date (labelled),
// the creation date, the topics and the author.
var facetAssignments = []string{`datePublished=date,facet,"Pub Date"`, "dateCreated=date,facet", "keywords=facet,fielded,Topics", "author=facet,fielded"}

func facetedFolder(t *testing.T, db *DB, host string, pages map[string]string) string {
	t.Helper()
	root := hostFolder(t, host, pages)
	fill(t, db, root)
	if err := db.AssignFields(context.Background(), root, facetAssignments); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BuildSets(context.Background(), "", 0); err != nil {
		t.Fatal(err)
	}
	return root
}

// Each date field is queried under its own name; nothing picks "the" date.
func TestEachDateFieldIsQueriedUnderItsOwnNameWithNoFacetNeeded(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, hostFolder(t, "f.example", datedPages))
	for q, want := range map[string][]string{
		"datepublished:2026":                               {"p1.md", "p2.md"},
		"datePublished:2026-04":                            {"p2.md"},
		"datepublished:2025":                               nil, // p3's date is not a date: no value
		"datecreated:2025":                                 {"p1.md", "p3.md"},
		"datecreated:[2025-06 TO 2026-06}":                 {"p2.md", "p3.md"},
		"datecreated:{2025-01 TO 2026-04}":                 {"p3.md"}, // after January 2025, before April 2026
		"datecreated:[2025-01 TO 2026-04}":                 {"p1.md", "p3.md"},
		"datepublished:[2026-03 TO 2026-04] AND compilers": {"p1.md", "p2.md"},
		"NOT datepublished:2026":                           {"p3.md", "p4.md"},
		"datecreated:[* TO *]":                             {"p1.md", "p2.md", "p3.md"}, // p4 has no date: never in a range
		"date:2026":                                        nil,                         // no field called date: the words date and 2026
	} {
		if got := hits(t, db, q, SearchOptions{}); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
	if len(db.IndexWarnings()) == 0 {
		t.Error("the unreadable date of p3 was not warned about")
	}
}

func TestADateFieldOneSetNamesIsNotAFieldInAnotherSet(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	fill(t, db, hostFolder(t, "a.example", datedPages))
	b := hostFolder(t, "b.example", map[string]string{"q1.md": "---\npub_date: 2026-09-09\n---\ncompilers q\n"})
	fill(t, db, b)
	db.AssignFields(ctx, b, []string{"pub_date=date"})
	db.BuildSets(ctx, "", 0)
	if got := hits(t, db, "pub_date:2026", SearchOptions{}); !reflect.DeepEqual(got, []string{"q1.md"}) {
		t.Errorf("pub_date:2026: %v", got)
	}
	if got := hits(t, db, "datepublished:2026", SearchOptions{}); !reflect.DeepEqual(got, []string{"p1.md", "p2.md"}) {
		t.Errorf("datepublished:2026 reads on set a only: %v", got)
	}
}

func facetOf(r *Results, field string) *FacetField {
	for i := range r.Facets {
		if r.Facets[i].Field == field {
			return &r.Facets[i]
		}
	}
	return nil
}

func values(f *FacetField) []string {
	var out []string
	if f == nil {
		return nil
	}
	for _, v := range f.Values {
		out = append(out, fmt.Sprintf("%s=%d", v.Value, v.Count))
	}
	return out
}

// No set has a facet until it says so, whatever front matter its pages have.
func TestThereAreNoFacetsUntilASetDefinesThem(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, hostFolder(t, "f.example", datedPages))
	r, err := db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 10})
	if err != nil || len(r.Hits) != 4 || len(r.Facets) != 0 {
		t.Errorf("%d hits, facets %v, %v", len(r.Hits), r.Facets, err)
	}
}

func TestAResultHasTheFacetsASetDefinedCountedOverTheWholeResultNotThePageShown(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	facetedFolder(t, db, "f.example", datedPages)
	r, err := db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 1}) // one hit shown, four found
	if err != nil || len(r.Hits) != 1 {
		t.Fatalf("%v %v", r, err)
	}
	for field, want := range map[string][]string{
		"keywords":      {"go=3", "oberon=1", "wasm=1"}, // most first, then by value
		"author":        {"ann=2", "bob=1"},
		"datepublished": {"2026=2"},           // p3's date is not one; p4 has none
		"datecreated":   {"2025=2", "2026=1"}, // years of the field, most first
	} {
		if got := values(facetOf(r, field)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", field, got, want)
		}
	}
	var order []string
	labels := map[string]string{}
	for _, f := range r.Facets {
		order = append(order, f.Field)
		labels[f.Field] = f.Label
	}
	if !reflect.DeepEqual(order, []string{"author", "datecreated", "datepublished", "keywords"}) {
		t.Errorf("facets by field name: %v", order)
	}
	if !reflect.DeepEqual(labels, map[string]string{"author": "author", "datecreated": "dateCreated", "datepublished": "Pub Date", "keywords": "Topics"}) {
		t.Errorf("labels %v", labels)
	}
}

// Following a facet is running its query: it finds exactly the pages that were
// counted.
func TestEveryFacetValuesQueryFindsExactlyTheCountedPages(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	facetedFolder(t, db, "f.example", datedPages)
	r, _ := db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 20})
	wantQuery := map[string]string{
		"keywords=go":        `(compilers) AND keywords:"go"`,
		"author=ann":         `(compilers) AND author:"ann"`,
		"datepublished=2026": `(compilers) AND datepublished:[2026 TO 2026]`,
		"datecreated=2025":   `(compilers) AND datecreated:[2025 TO 2025]`,
	}
	n := 0
	for _, f := range r.Facets {
		for _, v := range f.Values {
			n++
			if want, ok := wantQuery[f.Field+"="+v.Value]; ok && v.Query != want {
				t.Errorf("%s=%s: query %q, want %q", f.Field, v.Value, v.Query, want)
			}
			if got := hits(t, db, v.Query, SearchOptions{}); len(got) != v.Count {
				t.Errorf("%s=%s counted %d, its query finds %d (%v)", f.Field, v.Value, v.Count, len(got), got)
			}
		}
	}
	if n != 8 {
		t.Errorf("%d facet values, want 8", n)
	}
}

func TestAFacetHasAtMostTwentyValuesAndAQuoteInAValueDoesNotBreakItsQuery(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	pages := map[string]string{}
	for i := 1; i <= 25; i++ {
		pages[fmt.Sprintf("k%02d.md", i)] = fmt.Sprintf("---\nkeywords: [topic%02d, shared]\n---\nbody words\n", i)
	}
	pages["q.md"] = "---\nkeywords: ['say \"hi\" now']\n---\nbody words\n"
	facetedFolder(t, db, "f.example", pages)
	r, _ := db.SearchWith(context.Background(), "body", SearchOptions{Limit: 5})
	f := facetOf(r, "keywords")
	if f == nil || len(f.Values) != 20 || f.Values[0].Value != "shared" || f.Values[0].Count != 25 {
		t.Fatalf("%v", values(f))
	}
	r, _ = db.SearchWith(context.Background(), "body", SearchOptions{Limit: 100})
	for _, v := range facetOf(r, "keywords").Values {
		if strings.Contains(v.Value, `"`) && strings.Count(v.Query, `"`) != 2 {
			t.Errorf("a quote in %q reaches its query: %s", v.Value, v.Query)
		}
	}
	if got := hits(t, db, `body AND keywords:"say hi now"`, SearchOptions{}); !reflect.DeepEqual(got, []string{"q.md"}) {
		t.Errorf("%v", got)
	}
}

// A facet counts the pages of the sets that define it.
func TestFacetsAreSummedOverTheSetsThatDefineThemAndComeFromTheOneSetSearchedAlone(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	a := facetedFolder(t, db, "a.example", datedPages)
	b := facetedFolder(t, db, "b.example", map[string]string{"q1.md": "---\nkeywords: [go, rust]\n---\ncompilers q\n", "q2.md": "---\nkeywords: [rust]\n---\ncompilers w\n"})
	c := hostFolder(t, "c.example", map[string]string{"z1.md": "---\nkeywords: [go]\n---\ncompilers z\n"}) // defines nothing
	fill(t, db, c)
	r, _ := db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 20})
	if got := values(facetOf(r, "keywords")); !reflect.DeepEqual(got, []string{"go=4", "rust=2", "oberon=1", "wasm=1"}) {
		t.Errorf("a and b, not c: %v", got)
	}
	r, _ = db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 20, Set: b})
	if got := values(facetOf(r, "keywords")); !reflect.DeepEqual(got, []string{"rust=2", "go=1"}) {
		t.Errorf("b alone: %v", got)
	}
	r, _ = db.SearchWith(context.Background(), "compilers", SearchOptions{Limit: 20, Set: c})
	if len(r.Facets) != 0 {
		t.Errorf("c defines no facet: %v", r.Facets)
	}
	_ = a
}

func TestThereAreNoFacetsWithoutValuesOrWithoutAQuery(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	facetedFolder(t, db, "f.example", datedPages)
	if r, _ := db.SearchWith(ctx, "four", SearchOptions{Limit: 10}); len(r.Facets) != 0 { // p4 has no front matter
		t.Errorf("a result with no values: %v", r.Facets)
	}
	db.Tag(ctx, "https://f.example/p1.md", "x")
	if r, _ := db.SearchWith(ctx, "", SearchOptions{Limit: 10, Tags: []string{"x"}}); len(r.Facets) != 0 {
		t.Errorf("a listing with no query: %v", r.Facets)
	}
}
