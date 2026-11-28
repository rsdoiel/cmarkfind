package corpus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/index"
)

// hostFolder writes pages and an outline that lists them under https://HOST/ and
// returns the folder, which is the site key of a local fill.
func hostFolder(t *testing.T, host string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	var lines strings.Builder
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&lines, `<outline text="%s" url="https://%s/%s"/>`, name, host, name)
	}
	writeOutline(t, root, lines.String())
	return root
}

func generation(t *testing.T, dir string) uint64 {
	t.Helper()
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ix.Generation()
}

// The field mapping is an opaque blob the program stores and compares. A change
// of its bytes or its version makes the set's index out of date; nothing else
// is rebuilt; storing the same value again changes nothing.
func TestAChangedFieldMappingRebuildsThatSetAndNoOther(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", map[string]string{"a.md": "# A\n\nalpha\n"})
	b := hostFolder(t, "b.example", map[string]string{"b.md": "# B\n\nbeta\n"})
	fill(t, db, a)
	fill(t, db, b)
	ga, gb := generation(t, db.SetIndexDir(a)), generation(t, db.SetIndexDir(b))

	step := func(label string, version int, blob string, wantBuilt int) {
		t.Helper()
		if err := db.SetFields(ctx, a, version, blob); err != nil {
			t.Fatal(err)
		}
		r, err := db.BuildSets(ctx, "", 0)
		if err != nil || r.Built != wantBuilt || r.Unchanged != 2-wantBuilt {
			t.Fatalf("%s: %+v, %v", label, r, err)
		}
	}
	step("a new blob", 1, `{"author":["facet"]}`, 1)
	if g := generation(t, db.SetIndexDir(a)); g != ga+1 {
		t.Errorf("A generation %d, want %d", g, ga+1)
	}
	if g := generation(t, db.SetIndexDir(b)); g != gb {
		t.Errorf("B was rebuilt: %d, was %d", g, gb)
	}
	step("the same blob again", 1, `{"author":["facet"]}`, 0)
	step("a new version, the same bytes", 2, `{"author":["facet"]}`, 1)
	step("different bytes", 2, `{"author":["facet","fielded"]}`, 1)
	step("reset to the default", 0, "", 1)
	if err := db.SetFields(ctx, "https://nowhere.example/x", 1, "x"); !errors.Is(err, ErrNoSet) {
		t.Errorf("an unknown set: %v", err)
	}
}

// Reciprocal rank fusion, not one pooled score: the best page of a small set
// shares first place with the best page of a large one. Pooled BM25 would put
// every strong page of the large set above the small set's weak one.
func TestFusionGivesASmallSetsBestPageAPlaceBesideTheLargeSetsBest(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	small := hostFolder(t, "a.example", map[string]string{
		"weak.md": "# Notes\n\nAn otter was seen once in a very long wandering sentence about hills, fields, rivers, woods and weather.\n",
	})
	large := map[string]string{}
	for i := 1; i <= 6; i++ {
		large[fmt.Sprintf("s%d.md", i)] = fmt.Sprintf("# Otter %d\n\n%s\n", i, strings.Repeat("otter ", 12-i))
	}
	for i := 1; i <= 40; i++ { // pages without the word, so it is rare in the large set (a high idf there)
		large[fmt.Sprintf("f%d.md", i)] = fmt.Sprintf("# Filler %d\n\nnothing about it here, page %d.\n", i, i)
	}
	fill(t, db, small)
	fill(t, db, hostFolder(t, "b.example", large))
	got := addresses(searchLimit(t, db, "otter", 20))
	if len(got) != 7 {
		t.Fatalf("hits %v", got)
	}
	pos := -1
	for i, a := range got {
		if a == "https://a.example/weak.md" {
			pos = i
		}
	}
	if pos < 0 || pos > 1 {
		t.Errorf("the small set's page is at %d of %v; fusion puts it in the first two", pos, got)
	}
	// ranks within the large set keep their order
	last := -1
	for i, a := range got {
		if strings.HasPrefix(a, "https://b.example/") {
			if i < last {
				t.Errorf("large set out of order: %v", got)
			}
			last = i
		}
	}
}

func searchLimit(t *testing.T, db *DB, q string, n int) *Results {
	t.Helper()
	r, err := db.Search(context.Background(), q, n)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A set whose index is built with an analyzer this program does not read (a
// mirror, from Phase 7) is left out of the merge with one warning, never mixed;
// searched alone it is an error (65). A local set is simply rebuilt.
func TestASetWithAnAnalyzerThisProgramDoesNotReadIsLeftOutOfTheMergeWithAWarning(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	a := hostFolder(t, "a.example", map[string]string{"a.md": "# A\n\nzebra stripes\n"})
	b := hostFolder(t, "b.example", map[string]string{"b.md": "# B\n\nzebra herds\n"})
	fill(t, db, a)
	fill(t, db, b)
	if _, err := db.sql.Exec(`UPDATE sets SET source = 'mirror' WHERE site = ?`, a); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(db.SetIndexDir(a), "manifest.json")
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(mp, []byte(regexp.MustCompile(`"analyzer": "[^"]*"`).ReplaceAllString(string(raw), `"analyzer": "porter-stem-v1"`)), 0o644)

	r, err := db.Search(ctx, "zebra", 10)
	if err != nil {
		t.Fatalf("aggregate search: %v", err)
	}
	if got := addresses(r); len(got) != 1 || got[0] != "https://b.example/b.md" {
		t.Errorf("hits %v: only the set with a known analyzer is merged", got)
	}
	w := db.IndexWarnings()
	if len(w) != 1 || !strings.Contains(w[0], a) || !strings.Contains(w[0], "porter-stem-v1") {
		t.Errorf("warnings %q", w)
	}
	_, err = db.SearchWith(ctx, "zebra", SearchOptions{Set: a})
	if !index.IsAnalyzerError(err) || !index.IsDataError(err) {
		t.Errorf("searched alone: %v", err)
	}
	// a local set with the same fault is rebuilt, not skipped
	db.sql.Exec(`UPDATE sets SET source = 'local' WHERE site = ?`, a)
	if r, err := db.Search(ctx, "zebra", 10); err != nil || len(r.Hits) != 2 {
		t.Errorf("a local set is rebuilt: %v, %v", addresses(r), err)
	}
}
