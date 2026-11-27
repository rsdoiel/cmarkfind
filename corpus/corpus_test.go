package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/toc"
)

// site writes a site on disk with a toc.opml that lists the given pages by
// their published address (https://example.org/NAME), and the files. A page
// with a nil entry in files is listed in the outline but has no file.
func site(t *testing.T, files map[string]*string) string {
	t.Helper()
	root := t.TempDir()
	var lines strings.Builder
	for name, body := range files {
		fmt.Fprintf(&lines, `<outline text="%s" url="https://example.org/%s"/>`, name, name)
		if body != nil {
			if err := os.WriteFile(filepath.Join(root, name), []byte(*body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeOutline(t, root, lines.String())
	return root
}

func writeOutline(t *testing.T, root, outlines string) {
	t.Helper()
	dir := filepath.Join(root, ".well-known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body>` + outlines + `</body></opml>`
	if err := os.WriteFile(filepath.Join(dir, "toc.opml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func s(v string) *string { return &v }

func open(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corpus.db") // a real file, not :memory:
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func fill(t *testing.T, db *DB, root string) *FillReport {
	t.Helper()
	r, err := db.FillLocal(context.Background(), root, toc.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func search(t *testing.T, db *DB, q string) *Results {
	t.Helper()
	r, err := db.Search(context.Background(), q, 20)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return r
}

func addresses(r *Results) []string {
	var a []string
	for _, h := range r.Hits {
		a = append(a, h.Address)
	}
	return a
}

var threePages = map[string]*string{
	"a.md": s("---\ntitle: Otters\ndescription: River animals\n---\nThe otter swims in the river.\n"),
	"b.md": s("# Badgers\n\nBadgers dig setts in the woods.\n"),
	"c.md": s("# Cranes\n\nA crane stands in the shallow water.\n"),
}

func TestFillThreePageSiteThenSearchFindsThePageWithASnippet(t *testing.T) {
	db, _ := open(t)
	r := fill(t, db, site(t, threePages))
	if r.Added != 3 {
		t.Fatalf("report %+v", r)
	}
	res := search(t, db, "badgers")
	if got := addresses(res); len(got) != 1 || got[0] != "https://example.org/b.md" {
		t.Fatalf("hits %v", got)
	}
	h := res.Hits[0]
	if !strings.Contains(strings.ToLower(h.Snippet), "badgers") || h.Tier != "own" || h.Title == "" {
		t.Errorf("hit %+v", h)
	}
	if res.Searched != 3 {
		t.Errorf("searched %d, want 3", res.Searched)
	}
}

func TestPhrasePrefixAndBooleanQueries(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	for q, want := range map[string]int{
		`"shallow water"`:  1,
		`"water shallow"`:  0,
		`otte*`:            1,
		`river NOT otter`:  0,
		`badgers OR crane`: 2,
		`the`:              3,
	} {
		if n := len(search(t, db, q).Hits); n != want {
			t.Errorf("%q: %d hits, want %d", q, n, want)
		}
	}
}

// A person's punctuation is never a syntax error.
func TestInputThatIsNotValidFTS5IsQuoted(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	for _, q := range []string{`"unclosed`, `a AND`, `(((`, `c++`, `x:y`, `-`, `*`, ``} {
		if _, err := db.Search(context.Background(), q, 10); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	if n := len(search(t, db, `"unclosed`).Hits); n != 0 {
		t.Errorf("unexpected hits: %d", n)
	}
}

func TestFrontMatterTitleAndDescriptionAreIndexedButNotTheYAMLKeys(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	if n := len(search(t, db, "animals").Hits); n != 1 { // the description
		t.Errorf("description: %d hits", n)
	}
	if n := len(search(t, db, "otters").Hits); n != 1 { // the title
		t.Errorf("title: %d hits", n)
	}
	if n := len(search(t, db, "description").Hits); n != 0 { // a YAML key
		t.Errorf("a front matter key matched: %d hits", n)
	}
}

func TestAnEditedPageIsUpdatedAndTheOldTextNoLongerMatches(t *testing.T) {
	db, _ := open(t)
	root := site(t, threePages)
	fill(t, db, root)
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("# Badgers\n\nNow about weasels.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := fill(t, db, root)
	if r.Updated != 1 || r.Unchanged != 2 {
		t.Errorf("report %+v", r)
	}
	if n := len(search(t, db, "setts").Hits); n != 0 {
		t.Errorf("old text still matches: %d", n)
	}
	if n := len(search(t, db, "weasels").Hits); n != 1 {
		t.Errorf("new text: %d", n)
	}
}

func TestAPageTheOutlineNoLongerListsIsGoneAndNotReturned(t *testing.T) {
	db, _ := open(t)
	root := site(t, threePages)
	fill(t, db, root)
	writeOutline(t, root, `<outline text="a" url="https://example.org/a.md"/><outline text="b" url="https://example.org/b.md"/>`)
	r := fill(t, db, root)
	if r.Gone != 1 {
		t.Errorf("report %+v", r)
	}
	if n := len(search(t, db, "crane").Hits); n != 0 {
		t.Errorf("a gone page was returned")
	}
	if res := search(t, db, "river"); res.Searched != 2 {
		t.Errorf("searched %d, want 2 (gone pages are not counted)", res.Searched)
	}
	// listed again, it comes back
	writeOutline(t, root, `<outline text="c" url="https://example.org/c.md"/>`)
	fill(t, db, root)
	if n := len(search(t, db, "crane").Hits); n != 1 {
		t.Errorf("a page listed again should be searchable, got %d", n)
	}
}

func TestARefillOfAnUnchangedSiteWritesNothing(t *testing.T) {
	db, path := open(t)
	root := site(t, threePages)
	fill(t, db, root)
	before := fetchedAts(t, path)
	r := fill(t, db, root)
	if r.Added+r.Updated+r.Gone != 0 || r.Unchanged != 3 {
		t.Errorf("report %+v", r)
	}
	if after := fetchedAts(t, path); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("fetched_at changed:\n%v\n%v", before, after)
	}
}

func fetchedAts(t *testing.T, path string) []string {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rows, err := raw.Query(`SELECT address || ' ' || fetched_at FROM pages ORDER BY address`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		rows.Scan(&v)
		out = append(out, v)
	}
	return out
}

func TestAPageWithNoFileIsSkippedAndCounted(t *testing.T) {
	db, _ := open(t)
	files := map[string]*string{"a.md": threePages["a.md"], "ghost.md": nil}
	r := fill(t, db, site(t, files))
	if r.Added != 1 || r.Missing != 1 {
		t.Errorf("report %+v", r)
	}
}

func TestADatabaseOfANewerSchemaIsRefused(t *testing.T) {
	db, path := open(t)
	db.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE meta SET value = '999' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := Open(path); !errors.Is(err, ErrSchema) {
		t.Errorf("err = %v, want ErrSchema", err)
	}
}

func TestAFileThatIsNotADatabaseIsRefusedAsSchemaNotACrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	os.WriteFile(path, []byte(strings.Repeat("not a database ", 500)), 0o644)
	if _, err := Open(path); err == nil {
		t.Error("opened a text file as a database")
	}
}

// WAL: a second connection reads while the first has written.
func TestTwoConnectionsReadWhileOneWrites(t *testing.T) {
	db, path := open(t)
	root := site(t, threePages)
	fill(t, db, root)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := os.WriteFile(filepath.Join(root, "d.md"), []byte("# Dingoes\n\nDingoes howl.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeOutline(t, root, `<outline text="a" url="https://example.org/a.md"/><outline text="d" url="https://example.org/d.md"/>`)
	fill(t, db, root)
	if n := len(search(t, other, "dingoes").Hits); n != 1 {
		t.Errorf("the second connection sees %d hits", n)
	}
}

func TestNoOutlineIsAnErrorTheCallerCanClassify(t *testing.T) {
	db, _ := open(t)
	_, err := db.FillLocal(context.Background(), t.TempDir(), toc.Limits{})
	if !errors.Is(err, toc.ErrNoOutline) {
		t.Errorf("err = %v, want toc.ErrNoOutline", err)
	}
}

// writeSiteAt writes files and an outline listing them under origin (ending in /).
func writeSiteAt(root, origin string, files map[string]string) error {
	var lines strings.Builder
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&lines, `<outline text="%s" url="%s%s"/>`, name, origin, name)
	}
	dir := filepath.Join(root, ".well-known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body>` + lines.String() + `</body></opml>`
	return os.WriteFile(filepath.Join(dir, "toc.opml"), []byte(src), 0o644)
}

func absOf(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
