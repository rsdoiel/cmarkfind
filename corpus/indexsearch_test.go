package corpus

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/index"
)

// Tests for the switch from FTS5 to the partitioned index: written before the
// code. They need DB.IndexDir, DB.BuildIndex, schema version 5 and a search
// that reads the index.

func tables(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rows, err := raw.Query(`SELECT name FROM sqlite_master`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out[n] = true
	}
	return out
}

func TestANewCorpusHasNoFTS5TableAndTheFillWritesTheIndexBesideIt(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, path := open(t)
	fill(t, db, site(t, threePages))
	for n := range tables(t, path) {
		if strings.HasPrefix(n, "pages_fts") || n == "pages_ai" || n == "pages_au" || n == "pages_ad" {
			t.Errorf("FTS5 leftover %q", n)
		}
	}
	if want := filepath.Join(filepath.Dir(path), "index"); db.IndexDir() != want {
		t.Errorf("index dir %q, want %q", db.IndexDir(), want)
	}
	ix, err := index.Open(db.IndexDir())
	if err != nil {
		t.Fatal(err)
	}
	if ix.Count() != 3 {
		t.Errorf("index holds %d pages", ix.Count())
	}
}

func TestTheIndexDirectoryFollowsTheEnvironment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("CMARKFIND_INDEX", dir)
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Error(err)
	}
}

func TestAFillThatChangesPagesRaisesTheGenerationAndOneThatChangesNothingDoesNot(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	root := site(t, threePages)
	fill(t, db, root)
	gen := func() uint64 {
		ix, err := index.Open(db.IndexDir())
		if err != nil {
			t.Fatal(err)
		}
		return ix.Generation()
	}
	g1 := gen()
	fill(t, db, root)
	if g := gen(); g != g1 {
		t.Errorf("an unchanged fill rebuilt: %d then %d", g1, g)
	}
	os.WriteFile(filepath.Join(root, "b.md"), []byte("# Badgers\n\nBadgers and voles.\n"), 0o644)
	fill(t, db, root)
	if g := gen(); g <= g1 {
		t.Errorf("a changed page did not rebuild: %d then %d", g1, g)
	}
	if n := len(search(t, db, "voles").Hits); n != 1 {
		t.Errorf("voles: %d hits", n)
	}
}

func TestForgetAndExcludeAndUnexcludeKeepTheIndexInStep(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	fill(t, db, site(t, threePages))
	if _, err := db.Exclude(ctx, "https://example.org/b"); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, db, "badgers").Hits); n != 0 {
		t.Errorf("an excluded page is still found: %d", n)
	}
	if _, err := db.ForgetAll(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, db, "otter").Hits); n != 0 {
		t.Errorf("a forgotten page is still found: %d", n)
	}
	if ix, err := index.Open(db.IndexDir()); err != nil || ix.Count() != 0 {
		t.Errorf("index after forget: %v %v", ix, err)
	}
}

func TestASearchBuildsAMissingIndex(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	if err := os.RemoveAll(db.IndexDir()); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, db, "otter").Hits); n != 1 {
		t.Errorf("%d hits", n)
	}
	if _, err := os.Stat(filepath.Join(db.IndexDir(), "manifest.json")); err != nil {
		t.Error("the index was not rebuilt:", err)
	}
}

func TestASearchRebuildsAnIndexThatIsNotThisCorpusOnes(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	// someone builds another index into the directory, a higher generation
	other := []index.Doc{{Address: "https://elsewhere.example/x.md", Title: "X", Body: "zebra"}}
	if err := index.Build(db.IndexDir(), other, index.BuildOptions{Generation: 99}); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, db, "zebra").Hits); n != 0 {
		t.Errorf("a foreign page was served: %d", n)
	}
	if n := len(search(t, db, "otter").Hits); n != 1 {
		t.Errorf("own page lost: %d", n)
	}
}

func TestASnippetBracketsTheHitWords(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	h := search(t, db, "shallow").Hits[0]
	if !strings.Contains(h.Snippet, "[shallow]") {
		t.Errorf("snippet %q", h.Snippet)
	}
	h = search(t, db, `"shallow water"`).Hits[0]
	if !strings.Contains(h.Snippet, "[shallow] [water]") {
		t.Errorf("phrase snippet %q", h.Snippet)
	}
}

func TestATagFilterWithAQueryKeepsOnlyTaggedMatchesAndCountsThePassingPages(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Tag(ctx, weakAddr, "x")
	r := searchWith(t, db, "otter", SearchOptions{Tags: []string{"x"}})
	if got := addresses(r); len(got) != 1 || got[0] != weakAddr || r.Filtered != 1 {
		t.Errorf("%v filtered %d", got, r.Filtered)
	}
}

func TestAQueryOverTheLimitIsAnErrorTheCallerCanClassify(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	_, err := db.Search(context.Background(), strings.Repeat("otter ", 20000), 10)
	if !index.IsQueryLimit(err) {
		t.Errorf("err %v", err)
	}
}

// FTS5-only syntax is read as the words it is made of, never an error.
func TestFTS5OnlySyntaxIsSearchedAsWordsAndNeverFails(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	for _, q := range []string{`NEAR(otter river)`, `^otter`, `{title}: otter`, `otter NOT`} {
		if _, err := db.Search(context.Background(), q, 10); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
}

const v4FTS = `
CREATE VIRTUAL TABLE pages_fts USING fts5(title, description, body, content='pages', content_rowid='rowid');
CREATE TRIGGER pages_ai AFTER INSERT ON pages BEGIN
	INSERT INTO pages_fts(rowid, title, description, body) VALUES (new.rowid, new.title, new.description, new.body);
END;
CREATE TRIGGER pages_ad AFTER DELETE ON pages BEGIN
	INSERT INTO pages_fts(pages_fts, rowid, title, description, body) VALUES ('delete', old.rowid, old.title, old.description, old.body);
END;
CREATE TRIGGER pages_au AFTER UPDATE ON pages BEGIN
	INSERT INTO pages_fts(pages_fts, rowid, title, description, body) VALUES ('delete', old.rowid, old.title, old.description, old.body);
	INSERT INTO pages_fts(rowid, title, description, body) VALUES (new.rowid, new.title, new.description, new.body);
END;
INSERT INTO pages_fts(pages_fts) VALUES ('rebuild');
`

// A schema 4 file has the FTS5 table and its triggers. Opening it drops them,
// keeps every page, pin, tag and exclusion, and the next search finds pages
// through a freshly built index.
func TestAVersion4FileUpgradesInPlaceDropsFTS5AndSearchesTheIndex(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, path := open(t)
	ctx := context.Background()
	root := site(t, rankSite2())
	fill(t, db, root)
	db.Pin(ctx, weakAddr)
	db.Tag(ctx, weakAddr, "x")
	db.Exclude(ctx, "https://example.org/zzz/")
	db.Close()
	os.RemoveAll(filepath.Join(filepath.Dir(path), "index"))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v4FTS + `UPDATE meta SET value = '4' WHERE key = 'schema_version'; DELETE FROM meta WHERE key LIKE 'index%'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open v4: %v", err)
	}
	defer db2.Close()
	for n := range tables(t, path) {
		if strings.HasPrefix(n, "pages_fts") || strings.HasPrefix(n, "pages_a") {
			t.Errorf("left behind: %q", n)
		}
	}
	r := search(t, db2, "otter")
	if len(r.Hits) != 2 || r.Hits[0].Address != weakAddr || !r.Hits[0].Pinned || len(r.Hits[0].Tags) != 1 {
		t.Errorf("hits %+v", r.Hits)
	}
	if ex, _ := db2.Excluded(ctx); len(ex) != 1 {
		t.Errorf("exclusions %v", ex)
	}
	// writing a page after the upgrade must not reach for the dropped table
	if _, err := db2.sql.Exec(`UPDATE pages SET title = 'Changed' WHERE address = ?`, otherAddr); err != nil {
		t.Errorf("update after upgrade: %v", err)
	}
	var v string
	db2.sql.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if v != "5" || SchemaVersion != 5 {
		t.Errorf("schema_version %q, constant %d", v, SchemaVersion)
	}
}

func rankSite2() map[string]*string {
	return map[string]*string{
		"strong.md": s("# Otter otter\n\nOtter otter otter otter.\n"),
		"weak.md":   s("# Rivers\n\nOnce an otter swam by in a long story about rivers, fields, hills and other matters.\n"),
		"other.md":  s("# Badgers\n\nBadgers dig setts.\n"),
	}
}

func TestAHitCarriesTheAnchorOfTheHeadingItsMatchIsUnder(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, map[string]*string{
		"w.md": s("# Waterbirds\n\nIntro words.\n\n## Shallow water\n\nThe crane wades here.\n\n## Other\n\nNothing.\n"),
	}))
	h := search(t, db, "crane").Hits[0]
	if h.Anchor != "shallow-water" {
		t.Errorf("anchor %q", h.Anchor)
	}
	if h := search(t, db, "intro").Hits[0]; h.Anchor != "" { // under the title heading: no fragment
		t.Errorf("anchor %q", h.Anchor)
	}
}

// A fill and a search (or two programs) may both find the index missing and
// build it at once. Neither may fail, and the result must be a sound index.
func TestTwoBuildsAtOnceLeaveASoundIndexAndNeitherFails(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	ctx := context.Background()
	errs := make(chan error, 8)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := db.BuildIndex(ctx, db.IndexDir(), 0)
			errs <- err
		}()
		go func() {
			_, err := db.Search(ctx, "otter", 10)
			errs <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent build or search: %v", err)
		}
	}
	if err := index.Verify(db.IndexDir()); err != nil {
		t.Errorf("index after concurrent builds: %v", err)
	}
	if n := len(search(t, db, "otter").Hits); n != 1 {
		t.Errorf("%d hits", n)
	}
}
