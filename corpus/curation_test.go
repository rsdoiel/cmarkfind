package corpus

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/toc"
)

// two local sites in one corpus, so forgetting one can be seen to leave the other.
func twoSites(t *testing.T, db *DB) (a, b string) {
	t.Helper()
	a = site(t, map[string]*string{"a.md": threePages["a.md"], "b.md": threePages["b.md"]})
	other := "---\ntitle: Dingoes\n---\nDingoes howl at night.\n"
	b = t.TempDir()
	if err := writeSiteAt(b, "https://other.example/", map[string]string{"d.md": other}); err != nil {
		t.Fatal(err)
	}
	fill(t, db, a)
	fill(t, db, b)
	return a, b
}

func TestForgetOneSiteLeavesTheOtherSearchable(t *testing.T) {
	db, _ := open(t)
	a, _ := twoSites(t, db)
	n, err := db.Forget(context.Background(), absOf(t, a))
	if err != nil || n != 2 {
		t.Fatalf("forgot %d, %v", n, err)
	}
	if len(search(t, db, "otter").Hits) != 0 || len(search(t, db, "badgers").Hits) != 0 {
		t.Error("forgotten pages are still found")
	}
	if len(search(t, db, "dingoes").Hits) != 1 {
		t.Error("the other site was touched")
	}
	if r := search(t, db, "dingoes"); r.Searched != 1 {
		t.Errorf("searched %d", r.Searched)
	}
	if n, _ := db.Forget(context.Background(), absOf(t, a)); n != 0 {
		t.Errorf("a second forget removed %d", n)
	}
}

func TestForgetAllEmptiesTheCorpusButKeepsExclusions(t *testing.T) {
	db, _ := open(t)
	twoSites(t, db)
	if _, err := db.Exclude(context.Background(), "https://example.org/zzz/"); err != nil {
		t.Fatal(err)
	}
	n, err := db.ForgetAll(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("forgot %d, %v", n, err)
	}
	if search(t, db, "the").Searched != 0 {
		t.Error("pages remain")
	}
	if ex, _ := db.Excluded(context.Background()); len(ex) != 1 {
		t.Errorf("exclusions %v", ex)
	}
}

func TestCountSiteDeletesNothing(t *testing.T) {
	db, _ := open(t)
	a, _ := twoSites(t, db)
	if n, err := db.CountSite(context.Background(), absOf(t, a)); err != nil || n != 2 {
		t.Errorf("count %d, %v", n, err)
	}
	if search(t, db, "otter").Searched != 3 {
		t.Error("CountSite deleted")
	}
}

func TestExcludeRemovesStoredPagesUnderThePrefixAndNoOthers(t *testing.T) {
	db, _ := open(t)
	twoSites(t, db)
	n, err := db.Exclude(context.Background(), "https://example.org/b")
	if err != nil || n != 1 {
		t.Fatalf("removed %d, %v", n, err)
	}
	if len(search(t, db, "badgers").Hits) != 0 || len(search(t, db, "otter").Hits) != 1 {
		t.Error("wrong pages removed")
	}
	// idempotent
	if n, err := db.Exclude(context.Background(), "https://example.org/b"); err != nil || n != 0 {
		t.Errorf("again: %d, %v", n, err)
	}
	if ex, _ := db.Excluded(context.Background()); len(ex) != 1 || ex[0].Prefix != "https://example.org/b" {
		t.Errorf("rules %v", ex)
	}
}

// a % or _ in a prefix is text, never a wildcard.
func TestAPrefixIsPlainTextNotAPattern(t *testing.T) {
	db, _ := open(t)
	twoSites(t, db)
	if n, _ := db.Exclude(context.Background(), "https://example.org/%"); n != 0 {
		t.Errorf("a %% matched %d pages", n)
	}
	if n, _ := db.Exclude(context.Background(), "https://example.org/_.md"); n != 0 {
		t.Errorf("an _ matched %d pages", n)
	}
}

func TestExcludeRejectsWhatIsNotAnAddress(t *testing.T) {
	db, _ := open(t)
	for _, p := range []string{"", "example.org/x", "ftp://example.org/x", "https://", "/local/path"} {
		if _, err := db.Exclude(context.Background(), p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestLocalFillSkipsExcludedPagesAndCountsThem(t *testing.T) {
	db, _ := open(t)
	root := site(t, threePages)
	db.Exclude(context.Background(), "https://example.org/b.md")
	r := fill(t, db, root)
	if r.Added != 2 || r.Excluded != 1 {
		t.Errorf("report %+v", r)
	}
	if len(search(t, db, "badgers").Hits) != 0 {
		t.Error("an excluded page was stored")
	}
}

func TestRemoteFillSkipsExcludedPagesWithoutRequestingThem(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	db.Exclude(context.Background(), w.srv.URL+"/b.md")
	r, err := db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{})
	if err != nil || r.Added != 2 || r.Excluded != 1 {
		t.Fatalf("report %+v, %v", r, err)
	}
	if w.hit("/b.md") != 0 {
		t.Error("an excluded page was requested")
	}
}

func TestUnexcludeThenFillBringsThePageBack(t *testing.T) {
	db, _ := open(t)
	root := site(t, threePages)
	db.Exclude(context.Background(), "https://example.org/b.md")
	fill(t, db, root)
	ok, err := db.Unexclude(context.Background(), "https://example.org/b.md")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, _ := db.Unexclude(context.Background(), "https://example.org/b.md"); ok {
		t.Error("a second unexclude said it removed a rule")
	}
	if len(search(t, db, "badgers").Hits) != 0 {
		t.Error("unexclude alone must not bring pages back")
	}
	fill(t, db, root)
	if len(search(t, db, "badgers").Hits) != 1 {
		t.Error("the next fill should bring it back")
	}
}

func TestAVersion1FileIsUpgradedAndKeepsItsPages(t *testing.T) {
	db, path := open(t)
	fill(t, db, site(t, threePages))
	db.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TABLE excluded`, `UPDATE meta SET value = '1' WHERE key = 'schema_version'`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	defer db2.Close()
	if len(search(t, db2, "badgers").Hits) != 1 {
		t.Error("pages lost in the upgrade")
	}
	if _, err := db2.Exclude(context.Background(), "https://example.org/c.md"); err != nil {
		t.Errorf("exclude after upgrade: %v", err)
	}
	var v string
	db2.sql.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	// the upgrade runs to the current version (3 since cmarkfind DR-0004); this pinned "2"
	if v != strconv.Itoa(SchemaVersion) {
		t.Errorf("schema_version %q", v)
	}
}

func TestStatsCountsTheRules(t *testing.T) {
	db, _ := open(t)
	db.Exclude(context.Background(), "https://example.org/x/")
	db.Exclude(context.Background(), "https://example.org/y/")
	if st, _ := db.Stats(context.Background()); st.Excluded != 2 {
		t.Errorf("stats %+v", st)
	}
}

var _ = strings.Contains
