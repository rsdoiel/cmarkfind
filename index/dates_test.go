package index

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Phase 6c, the index: date fields answer ranges and dates; facets are counted
// over a set of documents.

func dateDocs() []Doc {
	mk := func(n, created string) Doc {
		d := Doc{Address: "https://x.example/" + n + ".md", Title: "T " + n, Body: "compilers " + n, Fields: map[string][]string{"author": {"Ann"}}}
		if created != "" {
			d.Fields["created"] = []string{created}
		}
		return d
	}
	return []Doc{mk("d1", "2026-11-27"), mk("d2", "2025-06"), mk("d3", ""), mk("d4", "2026-01-01"), mk("d5", "2026-12-31T23:59:00-08:00")}
}

var dateSpecs = []FieldSpec{{Name: "author", Facet: true, Fielded: true}, {Name: "created", Date: true}}

func dsearch(t *testing.T, ix *Index, q string) []string {
	t.Helper()
	n, err := ParseQueryFields(q, ix.QueryFields(), ix.DateFields())
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search(n, 0)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(h.Address, "https://x.example/"), ".md"))
	}
	sort.Strings(out)
	return out
}

func TestADateFieldAnswersRangesAndDatesAtDayPrecision(t *testing.T) {
	_, ix := buildFields(t, dateDocs(), dateSpecs)
	if !reflect.DeepEqual(ix.DateFields(), []string{"created"}) {
		t.Fatalf("date fields %v", ix.DateFields())
	}
	for q, want := range map[string][]string{
		"created:[2026 TO 2026]":             {"d1", "d4", "d5"},
		"created:2026":                       {"d1", "d4", "d5"},
		"created:2026-11":                    {"d1"},
		"created:2026-11-27":                 {"d1"},
		`created:"2026-11"`:                  {"d1"},
		"created:[2025-06 TO 2026-01]":       {"d2", "d4"},
		"created:[2026-01-01 TO 2026-01-01]": {"d4"},
		"created:{2026 TO 2028}":             nil,                // an exclusive year bound drops that whole year: only 2027 is left
		"created:[2026 TO 2027}":             {"d1", "d4", "d5"}, // the year 2026
		"created:{2025 TO 2027}":             {"d1", "d4", "d5"}, // 2026 is the only year between
		"created:{2025-06 TO 2026-12}":       {"d1", "d4"},       // after June 2025, before December 2026
		"created:[2026-01-01 TO 2026-12-31}": {"d1", "d4"},       // a full date excludes that day
		"created:{2026-01-01 TO 2026-12-31]": {"d1", "d5"},
		"created:[2025-06 TO 2026-07}":       {"d2", "d4"}, // June 2025 to June 2026
		"created:[* TO 2025]":                {"d2"},
		"created:[2026-12 TO *]":             {"d5"},
		"created:[* TO *]":                   {"d1", "d2", "d4", "d5"}, // a page with no date never matches
		"created:[2026 TO 2026] AND d1":      {"d1"},
		"NOT created:[2026 TO 2026]":         {"d2", "d3"},
		"created:2026 -created:2026-11":      {"d4", "d5"},
		"author:ann AND created:2025":        {"d2"},
		"created:2030":                       nil,
		"CREATED:[2026 TO 2026]":             {"d1", "d4", "d5"},
		"created:[2026 to 2026]":             {"d1", "d4", "d5"},
	} {
		if got := dsearch(t, ix, q); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
	// the zone of a timestamp is not applied: d5 is the 31st wherever it was written
	if got := dsearch(t, ix, "created:2026-12-31"); !reflect.DeepEqual(got, []string{"d5"}) {
		t.Errorf("created:2026-12-31: %v", got)
	}
}

func TestARangeIsTextWhenItCannotBeRead(t *testing.T) {
	_, ix := buildFields(t, dateDocs(), dateSpecs)
	for _, q := range []string{
		"created:[2026 TO banana]", "created:[banana TO 2026]", "created:[2026 TO", "created:banana", "created:[2026-13 TO 2027]",
		"author:[a TO z]", "title:[2026 TO 2027]", "nofield:[2026 TO 2027]", "created:[2026 2027]", "created:]2026 TO 2027[",
	} {
		n, err := ParseQueryFields(q, ix.QueryFields(), ix.DateFields())
		if err != nil || n == nil {
			t.Fatalf("%q: %v %v", q, n, err)
		}
		if strings.Contains(n.String(), " TO ") {
			t.Errorf("%q was read as a range: %s", q, n)
		}
		if _, err := ix.Search(n, 0); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	// the same text with no date fields given: created and 2026 are words
	n, _ := ParseQueryFields("created:[2026 TO 2026]", ix.QueryFields(), nil)
	if strings.Contains(n.String(), " TO ") {
		t.Errorf("a range without date fields: %s", n)
	}
	n, _ = ParseQuery("created:2026", ix.QueryFields())
	if strings.Contains(n.String(), "created:") {
		t.Errorf("ParseQuery has no date fields: %s", n)
	}
}

func TestARangeNodeIsWrittenAsDays(t *testing.T) {
	_, ix := buildFields(t, dateDocs(), dateSpecs)
	for q, want := range map[string]string{
		"created:[2026 TO 2026]":             "created:[2026-01-01 TO 2026-12-31]",
		"created:2026-06":                    "created:[2026-06-01 TO 2026-06-30]",
		"created:{2025 TO 2028}":             "created:[2026-01-01 TO 2027-12-31]",
		"created:[2026-01 TO 2026-07}":       "created:[2026-01-01 TO 2026-06-30]",
		"created:{2026-01 TO 2026-06}":       "created:[2026-02-01 TO 2026-05-31]",
		"created:[2026-01-01 TO 2026-06-30}": "created:[2026-01-01 TO 2026-06-29]",
		"created:[* TO 2025-06-15]":          "created:[* TO 2025-06-15]",
		"created:[2024-02 TO *]":             "created:[2024-02-01 TO *]",
	} {
		n, _ := ParseQueryFields(q, ix.QueryFields(), ix.DateFields())
		if n.String() != want {
			t.Errorf("%q: %s, want %s", q, n, want)
		}
	}
}

func TestARangeOnADateTheIndexDoesNotHaveMatchesNothing(t *testing.T) {
	_, plain := buildFields(t, dateDocs(), []FieldSpec{{Name: "author", Facet: true, Fielded: true}})
	n, _ := ParseQueryFields("created:2026", []string{"title", "description", "body", "created"}, []string{"created"})
	hits, err := plain.Search(n, 0)
	if err != nil || len(hits) != 0 {
		t.Errorf("%v, %v", hits, err)
	}
}

// A range beside a phrase through a document-only word is settled on the page
// (the fragment keeps the date text).
func TestARangeBesideADocumentOnlyPhraseIsStillExact(t *testing.T) {
	the := strings.Repeat("the ", 400)
	docs := []Doc{
		{Address: "https://x.example/p1.md", Title: "T", Body: the + "lion zebra", Fields: map[string][]string{"created": {"2026-05"}}},
		{Address: "https://x.example/p2.md", Title: "T", Body: the + "lion zebra", Fields: map[string][]string{"created": {"2024-05"}}},
		{Address: "https://x.example/p3.md", Title: "T", Body: the + "zebra"},
	}
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, EntryBytes: 128, Fields: []FieldSpec{{Name: "created", Date: true}}})
	if err != nil || len(rep.DocumentOnly) == 0 {
		t.Fatalf("%v %v", rep, err)
	}
	ix := open(t, dir)
	for q, want := range map[string][]string{
		`"the lion" created:2026`:   {"p1"},
		`"the lion" created:2024`:   {"p2"},
		`"the lion" -created:2026`:  {"p2"},
		`"the zebra" created:2026`:  nil,
		`"the zebra" -created:2026`: {"p3"}, // p3 has the phrase and no date
	} {
		if got := dsearch(t, ix, q); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}

// A date field with the facet role is a facet of years: it has a filter file
// with days and none with values.
func TestADateFieldWithTheFacetRoleIsAYearFacet(t *testing.T) {
	_, ix := buildFields(t, dateDocs(), []FieldSpec{{Name: "created", Date: true, Facet: true}, {Name: "author", Facet: true, Fielded: true}})
	if !reflect.DeepEqual(ix.FacetFields(), []string{"author", "created"}) {
		t.Errorf("facet fields %v", ix.FacetFields())
	}
	all := map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true}
	years, err := ix.YearCounts("created", all)
	if err != nil || !reflect.DeepEqual(years, map[int]int{2026: 3, 2025: 1}) {
		t.Errorf("years %v, %v", years, err)
	}
	if got, _ := ix.FacetCounts("created", all); len(got) != 0 {
		t.Errorf("a date facet has years, not values: %v", got)
	}
	if got, _ := ix.YearCounts("author", all); len(got) != 0 {
		t.Errorf("a field with no days has no years: %v", got)
	}
	if got := dsearch(t, ix, "created:2025"); !reflect.DeepEqual(got, []string{"d2"}) {
		t.Errorf("the year facet's field is still queried: %v", got)
	}
}

func TestFacetCountsAreOfTheDocumentsGiven(t *testing.T) {
	docs := []Doc{
		{Address: "https://x.example/a.md", Title: "A", Body: "x", Fields: map[string][]string{"keywords": {"Go", "Oberon"}, "created": {"2026-02-01"}}},
		{Address: "https://x.example/b.md", Title: "B", Body: "x", Fields: map[string][]string{"keywords": {"go", "wasm"}, "created": {"2026-07-01"}}},
		{Address: "https://x.example/c.md", Title: "C", Body: "x", Fields: map[string][]string{"keywords": {"Go"}, "created": {"2025-07-01"}}},
		{Address: "https://x.example/d.md", Title: "D", Body: "x"},
	}
	_, ix := buildFields(t, docs, []FieldSpec{{Name: "keywords", Facet: true, Fielded: true}, {Name: "created", Date: true}})
	if !reflect.DeepEqual(ix.FacetFields(), []string{"keywords"}) {
		t.Fatalf("facet fields %v", ix.FacetFields())
	}
	all := map[int]bool{0: true, 1: true, 2: true, 3: true}
	got, err := ix.FacetCounts("keywords", all)
	want := []FacetCount{{"go", 3}, {"oberon", 1}, {"wasm", 1}} // most first, then by value
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%v, %v", got, err)
	}
	if got, _ := ix.FacetCounts("keywords", map[int]bool{1: true, 2: true}); !reflect.DeepEqual(got, []FacetCount{{"go", 2}, {"wasm", 1}}) {
		t.Errorf("a subset: %v", got)
	}
	if got, _ := ix.FacetCounts("keywords", map[int]bool{3: true}); len(got) != 0 {
		t.Errorf("a page with no value: %v", got)
	}
	if got, err := ix.FacetCounts("nothing", all); err != nil || len(got) != 0 {
		t.Errorf("an unknown field: %v, %v", got, err)
	}
	years, err := ix.YearCounts("created", all)
	if err != nil || !reflect.DeepEqual(years, map[int]int{2026: 2, 2025: 1}) {
		t.Errorf("years %v, %v", years, err)
	}
	if years, _ := ix.YearCounts("created", map[int]bool{2: true, 3: true}); !reflect.DeepEqual(years, map[int]int{2025: 1}) {
		t.Errorf("a subset: %v", years)
	}
}
