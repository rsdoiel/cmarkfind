package corpus

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/toc"
)

const richPage = `---
title: Rich page
abstract: A short abstract.
author: R. S. Doiel
keywords: [Go, Oberon]
tags: "one, two"
year: 2026
draft: false
datePublished: 2026-11-27
nested:
  deeper: dropped
list_of_maps:
  - a: b
---
The body.
`

func frontMatterOf(t *testing.T, db *DB, address string) map[string]any {
	t.Helper()
	var raw string
	if err := db.sql.QueryRow(`SELECT meta FROM pages WHERE address = ?`, address).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" {
		t.Fatalf("%s has no meta", address)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("meta %q: %v", raw, err)
	}
	return m
}

// The corpus keeps a page's front matter: strings stay strings, numbers,
// booleans and YAML dates are their text, a list is a list of such strings and
// anything deeper is dropped. No front matter is {}.
func TestAFillKeepsTheFrontMatterOfEachPageAsJSON(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, map[string]*string{"rich.md": s(richPage), "plain.md": s("# Plain\n\nNo header.\n")}))
	m := frontMatterOf(t, db, "https://example.org/rich.md")
	want := map[string]any{
		"title": "Rich page", "abstract": "A short abstract.", "author": "R. S. Doiel",
		"keywords": []any{"Go", "Oberon"}, "tags": "one, two", "year": "2026", "draft": "false", "datePublished": "2026-11-27",
	}
	for k, v := range want {
		if got := m[k]; strings.Join(strs(got), "|") != strings.Join(strs(v), "|") {
			t.Errorf("%s = %v, want %v", k, got, v)
		}
	}
	if _, ok := m["nested"]; ok {
		t.Error("a nested map was kept")
	}
	if _, ok := m["list_of_maps"]; ok {
		t.Error("a list of maps was kept")
	}
	if got := frontMatterOf(t, db, "https://example.org/plain.md"); len(got) != 0 {
		t.Errorf("no front matter is {}, got %v", got)
	}
}

func strs(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, e.(string))
		}
		return out
	}
	return nil
}

// abstract stands in for description when a page has no description.
func TestAbstractIsTheDescriptionWhenThereIsNone(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, map[string]*string{
		"a.md": s("---\nabstract: From the abstract.\n---\nbody\n"),
		"b.md": s("---\ndescription: From the description.\nabstract: Ignored.\n---\nbody\n"),
	}))
	var a, b string
	db.sql.QueryRow(`SELECT description FROM pages WHERE address LIKE '%/a.md'`).Scan(&a)
	db.sql.QueryRow(`SELECT description FROM pages WHERE address LIKE '%/b.md'`).Scan(&b)
	if a != "From the abstract." || b != "From the description." {
		t.Errorf("a %q, b %q", a, b)
	}
}

// A file from before schema 7 has no meta column. Opening it adds the column
// (empty for every page); the next fill reads each page again even though its
// content is unchanged, and the one after that changes nothing.
func TestAVersion6FileGainsMetaAndTheNextFillRefreshesEveryPage(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, path := open(t)
	root := site(t, map[string]*string{"rich.md": s(richPage), "plain.md": s("# Plain\n\nNo header.\n")})
	fill(t, db, root)
	db.Close()
	raw := openRawFile(t, path)
	if _, err := raw.Exec(`ALTER TABLE pages DROP COLUMN meta; UPDATE meta SET value = '6' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var empty int
	db2.sql.QueryRow(`SELECT count(*) FROM pages WHERE meta = ''`).Scan(&empty)
	if empty != 2 {
		t.Fatalf("%d pages with an empty meta after the upgrade", empty)
	}
	r := fill(t, db2, root)
	if r.Updated != 2 || r.Unchanged != 0 || r.Added != 0 {
		t.Errorf("the refresh: %+v", r)
	}
	if m := frontMatterOf(t, db2, "https://example.org/rich.md"); m["author"] != "R. S. Doiel" {
		t.Errorf("meta %v", m)
	}
	if r := fill(t, db2, root); r.Unchanged != 2 || r.Updated != 0 {
		t.Errorf("the fill after: %+v", r)
	}
}

// A web page whose meta is empty is asked for again without If-None-Match, once.
func TestAWebPageWithNoMetaIsRefetchedOnceWithoutAValidator(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	w := newWebSite(t, map[string]string{"/a.md": "---\nauthor: Ann\n---\nBody.\n", "/b.md": "# B\n\nBody.\n"})
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if _, err := db.sql.Exec(`UPDATE pages SET meta = ''`); err != nil { // as after the upgrade
		t.Fatal(err)
	}
	r, err := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if err != nil || r.Updated != 2 || r.Unchanged != 0 {
		t.Fatalf("the refresh: %+v, %v", r, err)
	}
	if got := w.inm("/a.md"); got != "" {
		t.Errorf("the refetch sent If-None-Match %q", got)
	}
	if m := frontMatterOf(t, db, w.srv.URL+"/a.md"); m["author"] != "Ann" {
		t.Errorf("meta %v", m)
	}
	r, _ = fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if r.Unchanged != 2 || w.inm("/a.md") == "" {
		t.Errorf("the fill after: %+v, If-None-Match %q", r, w.inm("/a.md"))
	}
}

// ---- the mapping ----

func fieldsSet(t *testing.T) (*DB, string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	root := hostFolder(t, "a.example", map[string]string{"a.md": "---\ndate: 2026-11-27\nauthor: Ann\n---\nalpha\n"})
	fill(t, db, root)
	return db, root
}

func TestTheDefaultMappingMakesKeywordsAndAuthorFacetsAndTheDateFieldsDates(t *testing.T) {
	db, site := fieldsSet(t)
	m, err := db.FieldMapping(context.Background(), site)
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"keywords": "facet,fielded", "author": "facet,fielded",
		"date": "date", "datepublished": "date", "datemodified": "date", "pubdate": "date", "created": "date",
		"title": "built-in", "description": "built-in", "body": "built-in",
	} {
		if got := strings.Join(m.Roles(field), ","); got != want {
			t.Errorf("%s: %q, want %q", field, got, want)
		}
	}
	if got := m.Roles("tags"); len(got) != 0 {
		t.Errorf("tags is unmapped by default, got %v", got)
	}
	f := m.Fields()
	if !sort.StringsAreSorted(f) || len(f) != 10 {
		t.Errorf("fields %v", f)
	}
}

func TestAssignmentsOverrideTheDefaultAndNoneTurnsAFieldOff(t *testing.T) {
	db, site := fieldsSet(t)
	ctx := context.Background()
	if err := db.AssignFields(ctx, site, []string{"tags=facet,fielded", "Author=text", "keywords=none", "x-field=date,text"}); err != nil {
		t.Fatal(err)
	}
	m, _ := db.FieldMapping(ctx, site)
	for field, want := range map[string]string{
		"tags": "facet,fielded", "author": "text", "keywords": "", "x-field": "date,text", "datepublished": "date",
	} {
		if got := strings.Join(m.Roles(field), ","); got != want {
			t.Errorf("%s: %q, want %q", field, got, want)
		}
	}
	// roles come out in the order facet, fielded, date, text whatever order they went in
	db.AssignFields(ctx, site, []string{"tags=text,fielded,facet,date"})
	m, _ = db.FieldMapping(ctx, site)
	if got := strings.Join(m.Roles("tags"), ","); got != "facet,fielded,date,text" {
		t.Errorf("tags %q", got)
	}
	// FIELD= with no role is the same as none
	db.AssignFields(ctx, site, []string{"author="})
	if m, _ = db.FieldMapping(ctx, site); len(m.Roles("author")) != 0 {
		t.Errorf("author %v", m.Roles("author"))
	}
	// reset: no overrides at all
	if err := db.SetFields(ctx, site, 0, ""); err != nil {
		t.Fatal(err)
	}
	if m, _ = db.FieldMapping(ctx, site); strings.Join(m.Roles("author"), ",") != "facet,fielded" || len(m.Roles("tags")) != 0 {
		t.Errorf("after reset: author %v tags %v", m.Roles("author"), m.Roles("tags"))
	}
}

func TestABadRoleOrFieldNameOrABuiltInFieldIsRefusedAndChangesNothing(t *testing.T) {
	db, site := fieldsSet(t)
	ctx := context.Background()
	for _, bad := range [][]string{
		{"author=facets"}, {"author=facet,nonsense"}, {"=facet"}, {"no equals sign"}, {"bad name=facet"}, {"a/b=facet"},
		{"title=facet"}, {"description=text"}, {"body=none"}, {"abstract=facet"},
		{"author=facet", "keywords=wrong"}, // the first is not applied either
	} {
		if err := db.AssignFields(ctx, site, bad); !errors.Is(err, ErrBadFieldMapping) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	m, _ := db.FieldMapping(ctx, site)
	if got := strings.Join(m.Roles("author"), ","); got != "facet,fielded" {
		t.Errorf("a refused assignment changed author to %q", got)
	}
	if err := db.AssignFields(ctx, "https://nowhere.example/", []string{"a=facet"}); !errors.Is(err, ErrNoSet) {
		t.Errorf("an unknown set: %v", err)
	}
}

// The mapping changes the set's input, so the next build replaces its index; the
// same assignment again changes nothing.
func TestAChangedMappingRebuildsTheSetAndTheSameOneAgainDoesNot(t *testing.T) {
	db, site := fieldsSet(t)
	ctx := context.Background()
	built := func() int { b, _ := db.BuildSets(ctx, "", 0); return b.Built }
	if built() != 0 {
		t.Fatal("a fresh set needs no build")
	}
	db.AssignFields(ctx, site, []string{"tags=facet"})
	if built() != 1 {
		t.Error("a changed mapping did not rebuild")
	}
	db.AssignFields(ctx, site, []string{"tags=facet"})
	if built() != 0 {
		t.Error("the same assignment rebuilt")
	}
}

// ---- dates ----

// An unparsable date is warned about once per page and field and leaves the
// page found by its text; nothing else changes.
func TestAnUnparsableDateIsWarnedAboutAtBuildAndTheErrorIsNil(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	root := hostFolder(t, "a.example", map[string]string{
		"good.md": "---\ndatePublished: 2026-11-27\n---\nzebra good\n",
		"bad.md":  "---\ndatePublished: sometime last week\ncreated: 2026-13-45\n---\nzebra bad\n",
		"none.md": "zebra none\n",
	})
	if _, err := db.FillLocal(context.Background(), root, toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	w := db.IndexWarnings()
	text := strings.Join(w, "\n")
	for _, want := range []string{"https://a.example/bad.md", "datepublished", "sometime last week", "created", "2026-13-45", "2 dates"} {
		if !strings.Contains(text, want) {
			t.Errorf("warnings lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "good.md") || strings.Contains(text, "none.md") {
		t.Errorf("a good or absent date was warned about:\n%s", text)
	}
	if n := len(search(t, db, "zebra").Hits); n != 3 {
		t.Errorf("the pages are found by their text: %d hits", n)
	}
	if _, err := os.Stat(filepath.Join(setDirOf(t, db), "manifest.json")); err != nil {
		t.Errorf("the index was built: %v", err)
	}
	// the next build of an unchanged set says nothing
	db.BuildSets(context.Background(), "", 0)
	if w := db.IndexWarnings(); len(w) != 0 {
		t.Errorf("a build that built nothing warned: %q", w)
	}
}
