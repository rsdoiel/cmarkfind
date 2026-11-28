package corpus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 8 (cmarkfind DR-0018): what the server needs of the corpus: a total
// that does not depend on the page, an offset, a search that never builds, and
// a place for the server's settings.

func manyPages(n int, word string) map[string]string {
	m := map[string]string{}
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("p%03d.md", i)] = fmt.Sprintf("# Page %d\n\n%s on page %d\n", i, word, i)
	}
	return m
}

func addrsOf(r *Results) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.Address)
	}
	return out
}

func TestMetaStoresReadsAndDeletesAKeyAndKeepsTheSchemaVersion(t *testing.T) {
	db, path := open(t)
	ctx := context.Background()
	if v, ok, err := db.Meta(ctx, "server_port"); err != nil || ok || v != "" {
		t.Fatalf("an unset key: %q %v %v", v, ok, err)
	}
	if err := db.SetMeta(ctx, "server_port", "8123"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta(ctx, "server_port", "9000"); err != nil { // replaces
		t.Fatal(err)
	}
	if v, ok, err := db.Meta(ctx, "server_port"); err != nil || !ok || v != "9000" {
		t.Fatalf("after a replace: %q %v %v", v, ok, err)
	}
	db.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if v, ok, _ := again.Meta(ctx, "server_port"); !ok || v != "9000" {
		t.Errorf("not kept across a reopen: %q %v", v, ok)
	}
	if err := again.DeleteMeta(ctx, "server_port"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := again.Meta(ctx, "server_port"); ok {
		t.Error("a deleted key is still there")
	}
	if err := again.DeleteMeta(ctx, "never_set"); err != nil {
		t.Errorf("deleting a key that is not there: %v", err)
	}
	if v, ok, _ := again.Meta(ctx, "schema_version"); !ok || v == "" {
		t.Errorf("the schema version was lost: %q %v", v, ok)
	}
}

func TestTotalIsEveryMatchWhateverThePageAndOffsetCutsPagesWithoutGapsOrRepeats(t *testing.T) {
	db, _ := open(t)
	fill(t, db, hostFolder(t, "a.example", manyPages(250, "tomatoes")))
	ctx := context.Background()
	all, err := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 250})
	if err != nil || len(all.Hits) != 250 || all.Total != 250 {
		t.Fatalf("all: %d hits, total %d, %v", len(all.Hits), all.Total, err)
	}
	var walked []string
	for off := 0; off < 300; off += 100 {
		r, err := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 100, Offset: off})
		if err != nil {
			t.Fatal(err)
		}
		if r.Total != 250 {
			t.Errorf("offset %d: total %d, want 250", off, r.Total)
		}
		walked = append(walked, addrsOf(r)...)
		if want := []int{100, 100, 50}[off/100]; len(r.Hits) != want {
			t.Errorf("offset %d: %d hits, want %d", off, len(r.Hits), want)
		}
	}
	if strings.Join(walked, " ") != strings.Join(addrsOf(all), " ") {
		t.Error("three pages are not the one list, in order, with no gap or repeat")
	}
	past, err := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 100, Offset: 400})
	if err != nil || len(past.Hits) != 0 || past.Total != 250 {
		t.Errorf("past the end: %d hits, total %d, %v", len(past.Hits), past.Total, err)
	}
	small, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 3})
	if small.Total != 250 || len(small.Hits) != 3 {
		t.Errorf("a small page: %d hits, total %d", len(small.Hits), small.Total)
	}
	none, _ := db.SearchWith(ctx, "zebra", SearchOptions{Limit: 10})
	if none.Total != 0 || len(none.Hits) != 0 {
		t.Errorf("no match: %d hits, total %d", len(none.Hits), none.Total)
	}
}

func TestTotalCountsTheSetsSearchedAfterCurationAndNoneMore(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	fill(t, db, hostFolder(t, "a.example", manyPages(30, "tomatoes")))
	fill(t, db, hostFolder(t, "b.example", manyPages(20, "tomatoes")))
	if r, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 5}); r.Total != 50 {
		t.Errorf("two sets: total %d, want 50", r.Total)
	}
	sets, _ := db.Sets(ctx)
	var one string
	for _, s := range sets {
		if s.Pages == 20 {
			one = s.Name
		}
	}
	if r, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 5, Set: one}); r.Total != 20 {
		t.Errorf("one set: total %d, want 20", r.Total)
	}
	if _, err := db.Exclude(ctx, "https://a.example/p00"); err != nil { // p000 to p009
		t.Fatal(err)
	}
	if r, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 5}); r.Total != 40 {
		t.Errorf("after an exclusion of 10: total %d, want 40", r.Total)
	}
	if _, err := db.Pin(ctx, "https://b.example/p001.md"); err != nil {
		t.Fatal(err)
	}
	if err := func() error { _, e := db.Tag(ctx, "https://b.example/p002.md", "keep"); return e }(); err != nil {
		t.Fatal(err)
	}
	if r, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 5, Tags: []string{"keep"}}); r.Total != 1 {
		t.Errorf("tag filter: total %d, want 1", r.Total)
	}
	if r, _ := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 5, Pinned: true}); r.Total != 1 {
		t.Errorf("pinned filter: total %d, want 1", r.Total)
	}
}

func TestNoBuildLeavesOutASetThatNeedsAnIndexAndNeverWritesOne(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	fill(t, db, hostFolder(t, "a.example", manyPages(5, "tomatoes")))
	fill(t, db, hostFolder(t, "b.example", manyPages(7, "tomatoes")))
	sets, _ := db.Sets(ctx)
	if len(sets) != 2 {
		t.Fatalf("%d sets", len(sets))
	}
	stale := sets[0].Name
	if err := os.RemoveAll(db.SetIndexDir(stale)); err != nil {
		t.Fatal(err)
	}
	r, err := db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 50, NoBuild: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hits) != sets[1].Pages || r.Total != sets[1].Pages {
		t.Errorf("%d hits, total %d, want only the built set's %d", len(r.Hits), r.Total, sets[1].Pages)
	}
	if w := strings.Join(db.IndexWarnings(), "\n"); !strings.Contains(w, stale) {
		t.Errorf("no warning naming the set left out: %q", w)
	}
	if _, err := os.Stat(db.SetIndexDir(stale)); !os.IsNotExist(err) {
		t.Errorf("a search that may not build made %s: %v", db.SetIndexDir(stale), err)
	}
	// naming the set that needs building is an error, not an empty answer
	_, err = db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 50, NoBuild: true, Set: stale})
	if !errors.Is(err, ErrNeedsBuild) {
		t.Errorf("search of the one set that needs a build: %v, want ErrNeedsBuild", err)
	}
	// without the option a search builds, as it always has
	r, err = db.SearchWith(ctx, "tomatoes", SearchOptions{Limit: 50})
	if err != nil || r.Total != 12 {
		t.Errorf("a building search: total %d, %v", r.Total, err)
	}
	if _, err := os.Stat(filepath.Join(db.SetIndexDir(stale), "manifest.json")); err != nil {
		t.Errorf("the normal search did not build it: %v", err)
	}
}
