package index

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Phase 6b: filter files. A field mapped facet, fielded or date has one filter
// file (values, terms, days) listed in the manifest; a clause field:word is
// answered from the terms of that field.

func fieldDocs() []Doc {
	return []Doc{
		{Address: "https://x.example/d1.md", Title: "Oberon notes", Body: "compilers and runtimes", Fields: map[string][]string{
			"author": {"R. S. Doiel"}, "keywords": {"Go", "Oberon"}, "created": {"2026-11-27"}}},
		{Address: "https://x.example/d2.md", Title: "Other notes", Body: "compilers and gardens", Fields: map[string][]string{
			"author": {"Ann Example"}, "keywords": {"go", "wasm"}, "created": {"2025-06"}}},
		{Address: "https://x.example/d3.md", Title: "Plain", Body: "gardens only"},
	}
}

var fieldSpecs = []FieldSpec{
	{Name: "author", Facet: true, Fielded: true},
	{Name: "keywords", Facet: true, Fielded: true},
	{Name: "created", Date: true},
}

func buildFields(t *testing.T, docs []Doc, specs []FieldSpec) (string, *Index) {
	t.Helper()
	dir := t.TempDir()
	if err := Build(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, Fields: specs}); err != nil {
		t.Fatal(err)
	}
	return dir, open(t, dir)
}

// fsearch parses q with the fields the index offers, as the corpus does.
func fsearch(t *testing.T, ix *Index, q string) []string {
	t.Helper()
	n, err := ParseQuery(q, ix.QueryFields())
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search(n, 0)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, strings.TrimPrefix(h.Address, "https://x.example/"))
	}
	sort.Strings(out)
	return out
}

func TestAFieldedClauseFindsPagesByTheWordsOfTheFieldsValues(t *testing.T) {
	_, ix := buildFields(t, fieldDocs(), fieldSpecs)
	for q, want := range map[string][]string{
		"author:doiel":             {"d1.md"},
		"author:DOIEL":             {"d1.md"},
		`author:"r. s. doiel"`:     {"d1.md"},
		`author:"doiel r s"`:       {"d1.md"}, // the words of the value; their order is not kept
		"author:doi*":              {"d1.md"},
		"author:ann":               {"d2.md"},
		"keywords:go":              {"d1.md", "d2.md"},
		"keywords:oberon":          {"d1.md"},
		"keywords:wasm":            {"d2.md"},
		"author:ann AND compilers": {"d2.md"},
		"author:(ann OR doiel)":    {"d1.md", "d2.md"},
		"author:ann OR gardens":    {"d2.md", "d3.md"},
		"NOT author:doiel":         {"d2.md", "d3.md"},
		"compilers -author:ann":    {"d1.md"},
		"title:oberon keywords:go": {"d1.md"},
		"author:nobody":            nil,
	} {
		if got := fsearch(t, ix, q); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}

func TestAValueWithPunctuationAndAccentsIsFoundByItsFoldedWords(t *testing.T) {
	docs := []Doc{{Address: "https://x.example/d1.md", Title: "T", Body: "b", Fields: map[string][]string{"author": {"Müller, José-María"}}}}
	_, ix := buildFields(t, docs, []FieldSpec{{Name: "author", Facet: true, Fielded: true}})
	for _, q := range []string{`author:müller`, `author:MÜLLER`, `author:"josé maría"`, `author:"MÜLLER JOSÉ MARÍA"`, `author:jos*`} {
		if got := fsearch(t, ix, q); len(got) != 1 {
			t.Errorf("%q: %v", q, got)
		}
	}
}

func TestAFieldTheIndexDoesNotHaveIsTextForTheParserAndNothingForAnIndexThatIsAskedAboutIt(t *testing.T) {
	_, ix := buildFields(t, fieldDocs(), fieldSpecs)
	if got := fsearch(t, ix, "foo:bar"); got != nil { // not a field: the words foo and bar
		t.Errorf("foo:bar: %v", got)
	}
	n, _ := ParseQuery("foo:bar", ix.QueryFields())
	if strings.Contains(n.String(), "foo:") {
		t.Errorf("foo was taken for a field: %s", n)
	}
	// an index with no filters: the parser reads author:x as text
	_, plain := buildFields(t, fieldDocs(), nil)
	if len(plain.FilterFields()) != 0 || !reflect.DeepEqual(plain.QueryFields(), []string{"title", "description", "body"}) {
		t.Errorf("filters %v, query fields %v", plain.FilterFields(), plain.QueryFields())
	}
	n, _ = ParseQuery("author:doiel", plain.QueryFields())
	if strings.Contains(n.String(), "author:") {
		t.Errorf("author was taken for a field: %s", n)
	}
	// a query parsed for a wider set of fields, asked of an index without one: no match, no error
	wide, _ := ParseQuery("author:doiel", []string{"title", "description", "body", "author"})
	hits, err := plain.Search(wide, 0)
	if err != nil || len(hits) != 0 {
		t.Errorf("an unmapped field on this index: %v, %v", hits, err)
	}
	// a field that is only a facet or a date has no terms: fielded clauses on it match nothing
	_, ix2 := buildFields(t, fieldDocs(), []FieldSpec{{Name: "author", Facet: true}})
	wide2, _ := ParseQuery("author:doiel", []string{"title", "description", "body", "author"})
	if hits, err := ix2.Search(wide2, 0); err != nil || len(hits) != 0 {
		t.Errorf("a facet that is not fielded: %v, %v", hits, err)
	}
}

func TestTheIndexListsItsFilterFilesAndTheFieldsAQueryMayName(t *testing.T) {
	dir, ix := buildFields(t, fieldDocs(), fieldSpecs)
	want := []FilterInfo{{"author", true, true, false}, {"created", false, false, true}, {"keywords", true, true, false}}
	if got := ix.FilterFields(); !reflect.DeepEqual(got, want) {
		t.Errorf("filters %v, want %v", got, want)
	}
	if got := ix.QueryFields(); !reflect.DeepEqual(got, []string{"title", "description", "body", "author", "keywords"}) {
		t.Errorf("query fields %v", got)
	}
	m, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if !strings.Contains(string(m), `"filters"`) || strings.Contains(string(m), `"required"`) {
		t.Errorf("the manifest names the filters and requires nothing: %s", m)
	}
	// an index with no mapped field has no filters key and no filter folder
	plainDir, _ := buildFields(t, fieldDocs(), nil)
	pm, _ := os.ReadFile(filepath.Join(plainDir, "manifest.json"))
	if strings.Contains(string(pm), "filters") {
		t.Errorf("filters in an index without any: %s", pm)
	}
	if _, err := os.Stat(filepath.Join(plainDir, "x")); !os.IsNotExist(err) {
		t.Errorf("an x folder in an index without filters (%v)", err)
	}
}

func TestAFieldNoPageHasAValueForMakesNoFile(t *testing.T) {
	dir, ix := buildFields(t, fieldDocs(), append(append([]FieldSpec{}, fieldSpecs...), FieldSpec{Name: "isbn", Facet: true, Fielded: true}))
	for _, f := range ix.FilterFields() {
		if f.Name == "isbn" {
			t.Errorf("isbn has no values anywhere, yet it has a filter file")
		}
	}
	if err := Verify(dir); err != nil {
		t.Error(err)
	}
}

func TestFilterFilesAreCheckedByVerifyAndPrunedWithTheRest(t *testing.T) {
	dir, _ := buildFields(t, fieldDocs(), fieldSpecs)
	var files []string
	filepath.WalkDir(filepath.Join(dir, "x"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 3 {
		t.Fatalf("%d filter files, want 3 (author, created, keywords)", len(files))
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// a rebuild with different values replaces them; Prune takes the old ones away and keeps the new
	docs := fieldDocs()
	docs[0].Fields = map[string][]string{"author": {"Someone Else"}, "keywords": {"go"}, "created": {"2024"}}
	if err := Build(dir, docs, BuildOptions{Generation: 2, Built: fixedTime, Fields: fieldSpecs}); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneOlderThan(dir, 0); err != nil {
		t.Fatal(err)
	}
	var after int
	filepath.WalkDir(filepath.Join(dir, "x"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			after++
		}
		return nil
	})
	if after != 3 {
		t.Errorf("%d filter files after the prune, want 3", after)
	}
	if err := Verify(dir); err != nil {
		t.Errorf("verify after the prune: %v", err)
	}
	if got := fsearch(t, open(t, dir), "author:someone"); !reflect.DeepEqual(got, []string{"d1.md"}) {
		t.Errorf("after the rebuild: %v", got)
	}
	// damage one: Verify and the search both refuse it
	b, _ := os.ReadFile(files[0])
	_ = b
	var victim string
	filepath.WalkDir(filepath.Join(dir, "x"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && victim == "" {
			victim = p
		}
		return nil
	})
	data, _ := os.ReadFile(victim)
	data[len(data)/2] ^= 0xff
	os.WriteFile(victim, data, 0o644)
	if err := Verify(dir); !IsDataError(err) {
		t.Errorf("verify of a damaged filter file: %v", err)
	}
}

func TestTheSameInputsGiveTheSameFilterFiles(t *testing.T) {
	list := func(dir string) map[string]string {
		out := map[string]string{}
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				b, _ := os.ReadFile(p)
				rel, _ := filepath.Rel(dir, p)
				out[rel] = string(b)
			}
			return nil
		})
		return out
	}
	a, _ := buildFields(t, fieldDocs(), fieldSpecs)
	b, _ := buildFields(t, fieldDocs(), fieldSpecs)
	if !reflect.DeepEqual(list(a), list(b)) {
		t.Error("two builds of the same pages differ")
	}
}

// A phrase through a word held without positions is settled on the page's
// words; a fielded clause beside it must be settled too (from the values the
// page keeps), not dropped.
func TestAFieldedClauseBesideADocumentOnlyPhraseIsStillExact(t *testing.T) {
	the := strings.Repeat("the ", 400)
	docs := []Doc{
		{Address: "https://x.example/p1.md", Title: "T", Body: the + "lion zebra", Fields: map[string][]string{"author": {"Ann"}}},
		{Address: "https://x.example/p2.md", Title: "T", Body: the + "lion zebra", Fields: map[string][]string{"author": {"Bob"}}},
		{Address: "https://x.example/p3.md", Title: "T", Body: the + "zebra", Fields: map[string][]string{"author": {"Ann"}}},
	}
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, EntryBytes: 128, Fields: []FieldSpec{{Name: "author", Facet: true, Fielded: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DocumentOnly) == 0 {
		t.Fatal("the word the was not made document-only; the test does not test what it says")
	}
	ix := open(t, dir)
	for q, want := range map[string][]string{
		`"the lion" author:ann`:   {"p1.md"},
		`"the lion" author:bob`:   {"p2.md"},
		`"the zebra" author:ann`:  {"p3.md"},
		`"the zebra" author:bob`:  nil,
		`"lion zebra" author:bob`: {"p2.md"},
		`"the zebra" -author:ann`: nil,
	} {
		if got := fsearch(t, ix, q); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}
