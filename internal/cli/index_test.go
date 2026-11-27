package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/index"
)

// built fills a corpus, builds its index into DIR and returns both.
func built(t *testing.T) (db, dir string) {
	t.Helper()
	db = filled(t)
	dir = filepath.Join(t.TempDir(), "idx")
	if r := do(t, "-db", db, "indexer", "build", dir); r.code != 0 {
		t.Fatalf("index build: %+v", r)
	}
	return db, dir
}

func TestIndexBuildAndSearch(t *testing.T) {
	db, dir := built(t)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	r := do(t, "-db", db, "search", "-index", dir, "otter")
	if r.code != 0 || !strings.Contains(r.out, "https://example.org/a.md") || strings.Contains(r.out, "b.md") {
		t.Errorf("otter: %+v", r)
	}
	if !strings.Contains(r.out, "Otters") {
		t.Errorf("title missing: %q", r.out)
	}
	if r := do(t, "-db", db, "search", "-index", dir, `"shallow water" OR badgers`); r.code != 0 ||
		!strings.Contains(r.out, "c.md") || !strings.Contains(r.out, "b.md") || strings.Contains(r.out, "a.md") {
		t.Errorf("boolean: %+v", r)
	}
	if r := do(t, "-db", db, "search", "-index", dir, "title:crane*"); r.code != 0 || !strings.Contains(r.out, "c.md") {
		t.Errorf("field prefix: %+v", r)
	}
}

func TestIndexSearchJSONAndLimit(t *testing.T) {
	db, dir := built(t)
	r := do(t, "-db", db, "search", "-index", dir, "-json", "-limit", "1", "the")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var v struct {
		Hits []struct{ Address, Title string }
	}
	if err := json.Unmarshal([]byte(r.out), &v); err != nil || len(v.Hits) != 1 || v.Hits[0].Address == "" {
		t.Errorf("json %q: %v", r.out, err)
	}
}

func TestIndexSearchNoMatchIsOne(t *testing.T) {
	db, dir := built(t)
	if r := do(t, "-db", db, "search", "-index", dir, "zebra"); r.code != 1 {
		t.Errorf("no match: %+v", r)
	}
	if r := do(t, "-db", db, "search", "-index", dir, "--", "---"); r.code != 1 { // present, nothing searchable
		t.Errorf("punctuation: %+v", r)
	}
}

func TestIndexSearchUsageErrorsAreTwo(t *testing.T) {
	db, dir := built(t)
	for name, args := range map[string][]string{
		"no query":   {"search", "-index", dir},
		"empty":      {"search", "-index", dir, ""},
		"too long":   {"search", "-index", dir, strings.Repeat("a ", 600)},
		"too many":   {"search", "-index", dir, strings.Repeat("a ", 70)},
		"bad limit":  {"search", "-index", dir, "-limit", "0", "a"},
		"index flag": {"search", "-index"},
	} {
		r := do(t, append([]string{"-db", db}, args...)...)
		if r.code != 2 {
			t.Errorf("%s: exit %d, %q", name, r.code, r.errOut)
		}
	}
}

func TestIndexMissingIsSixtySix(t *testing.T) {
	db := filled(t)
	none := filepath.Join(t.TempDir(), "nope")
	for _, args := range [][]string{
		{"search", "-index", none, "otter"},
		{"indexer", "validate", none},
		{"indexer", "list", none},
	} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 66 {
			t.Errorf("%v: exit %d, %q", args, r.code, r.errOut)
		}
	}
	// build needs the corpus
	if r := do(t, "-db", filepath.Join(t.TempDir(), "none.db"), "indexer", "build", filepath.Join(t.TempDir(), "i")); r.code != 66 {
		t.Errorf("build without corpus: %+v", r)
	}
}

func TestIndexDamagedIsSixtyFive(t *testing.T) {
	db, dir := built(t)
	// a data file changed after the build: verify and search both refuse it
	var victim string
	filepath.WalkDir(filepath.Join(dir, "w"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && victim == "" {
			victim = p
		}
		return nil
	})
	b, _ := os.ReadFile(victim)
	b[len(b)/2] ^= 0xff
	os.WriteFile(victim, b, 0o644)
	if r := do(t, "-db", db, "indexer", "validate", dir); r.code != 65 {
		t.Errorf("validate: %+v", r)
	}
	// a manifest that is not JSON
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "manifest.json"), []byte("{nope"), 0o644)
	for _, args := range [][]string{{"search", "-index", bad, "otter"}, {"indexer", "validate", bad}, {"indexer", "list", bad}} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 65 {
			t.Errorf("%v: exit %d, %q", args, r.code, r.errOut)
		}
	}
}

func TestIndexValidateOK(t *testing.T) {
	db, dir := built(t)
	if r := do(t, "-db", db, "indexer", "validate", dir); r.code != 0 {
		t.Errorf("%+v", r)
	}
}

func TestIndexListSaysWhatIsThere(t *testing.T) {
	db, dir := built(t)
	r := do(t, "-db", db, "indexer", "list", dir)
	if r.code != 0 || !strings.Contains(r.out, "pages 3") || !strings.Contains(r.out, "generation 1") {
		t.Errorf("%+v", r)
	}
	// bare `index` is the list, of the default directory
	r = do(t, "-db", db, "indexer")
	if r.code != 66 {
		t.Errorf("no default index yet: %+v", r)
	}
	r = do(t, "-db", db, "indexer", "list", "-json", dir)
	var v map[string]any
	if json.Unmarshal([]byte(r.out), &v) != nil || v["pages"] != float64(3) || v["generation"] != float64(1) {
		t.Errorf("json: %q", r.out)
	}
}

func TestIndexDefaultDirectoryIsBesideTheCorpus(t *testing.T) {
	db := filled(t)
	t.Setenv("CMARKFIND_INDEX", "")
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(db), "index", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if r := do(t, "-db", db, "indexer"); r.code != 0 || !strings.Contains(r.out, "pages 3") {
		t.Errorf("list default: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate"); r.code != 0 {
		t.Errorf("verify default: %+v", r)
	}
	// the environment names another
	other := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("CMARKFIND_INDEX", other)
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(other, "manifest.json")); err != nil {
		t.Error("CMARKFIND_INDEX not used")
	}
}

func TestIndexRebuildRaisesGenerationAndPrunes(t *testing.T) {
	db, dir := built(t)
	count := func() (n int) {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return
	}
	before := count()
	// change a page so the files differ
	raw, _ := openRaw(db)
	raw.Exec(`UPDATE pages SET body = 'The otter swims and dives.' WHERE address LIKE '%a.md'`)
	raw.Close()
	if r := do(t, "-db", db, "indexer", "build", dir); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := do(t, "-db", db, "indexer", "list", dir); !strings.Contains(r.out, "generation 2") {
		t.Errorf("%+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate", dir); r.code != 0 {
		t.Errorf("verify after rebuild: %+v", r)
	}
	if after := count(); after > before {
		t.Errorf("old files were left: %d files before, %d after", before, after)
	}
	if r := do(t, "-db", db, "search", "-index", dir, "dives"); r.code != 0 {
		t.Errorf("new text not found: %+v", r)
	}
}

func TestIndexBuildAppliesExclusionsAndGone(t *testing.T) {
	db := filled(t)
	raw, _ := openRaw(db)
	// excluded by a rule written behind exclude's back, and one page gone
	raw.Exec(`INSERT INTO excluded(prefix, added_at) VALUES ('https://example.org/b', 'x')`)
	raw.Exec(`UPDATE pages SET gone_at = 'x' WHERE address LIKE '%c.md'`)
	raw.Close()
	dir := filepath.Join(t.TempDir(), "idx")
	if r := do(t, "-db", db, "indexer", "build", dir); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, w := range []string{"badgers", "crane"} {
		if r := do(t, "-db", db, "search", "-index", dir, w); r.code != 1 {
			t.Errorf("%s still indexed: %+v", w, r)
		}
	}
	if r := do(t, "-db", db, "search", "-index", dir, "otter"); r.code != 0 {
		t.Errorf("otter lost: %+v", r)
	}
}

func TestBareIndexVerbPointsToIndexer(t *testing.T) {
	r := do(t, "index", "build")
	if r.code != 2 || !strings.Contains(r.errOut, "indexer") {
		t.Errorf("%+v", r)
	}
}

func TestIndexBuildUsageErrors(t *testing.T) {
	db := filled(t)
	d := t.TempDir()
	for name, args := range map[string][]string{
		"two dirs":        {"indexer", "build", d, d},
		"bad chunk":       {"indexer", "build", "-chunk", "0", d},
		"unknown verb":    {"indexer", "frob"},
		"validate 2 dirs": {"indexer", "validate", d, d},
	} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 2 {
			t.Errorf("%s: exit %d, %q", name, r.code, r.errOut)
		}
	}
}

func TestIndexBuildCantCreate(t *testing.T) {
	db := filled(t)
	f := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(f, []byte("x"), 0o644)
	if r := do(t, "-db", db, "indexer", "build", filepath.Join(f, "sub")); r.code != 73 {
		t.Errorf("exit %d, %q", r.code, r.errOut)
	}
}

func TestClassifyIndexErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
		cls  string
	}{
		{fmt.Errorf("x: %w", index.ErrData), 65, "data"},
		{fmt.Errorf("x: %w", index.ErrMissing), 66, "no_input"},
		{fmt.Errorf("x: %w", index.ErrQueryLimit), 2, "usage"},
	} {
		if code, cls := Classify(c.err); code != c.code || cls != c.cls {
			t.Errorf("%v: %d %s", c.err, code, cls)
		}
	}
}

func TestExitStatusDocumentsIndexCodes(t *testing.T) {
	for _, topic := range []string{"exit-status", "indexer"} {
		r := do(t, "help", topic)
		if r.code != 0 {
			t.Fatalf("help %s: %+v", topic, r)
		}
		for _, want := range []string{"index", "EXIT STATUS"} {
			if topic == "exit-status" && want == "index" {
				continue // checked below, by sentence
			}
			if !strings.Contains(r.out, want) {
				t.Errorf("help %s lacks %q", topic, want)
			}
		}
	}
	r := do(t, "help", "exit-status")
	for _, want := range []string{"index is damaged", "index is missing", "query is too large"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("exit-status lacks %q", want)
		}
	}
}

func TestIndexSearchShowsAnExcerpt(t *testing.T) {
	db, dir := built(t)
	r := do(t, "-db", db, "search", "-index", dir, "otter")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.out, "The otter swims in the river") {
		t.Errorf("no excerpt in %q", r.out)
	}
	if strings.Contains(r.out, "<mark>") {
		t.Errorf("a terminal does not get HTML: %q", r.out)
	}
}

func TestIndexSearchJSONHasTheMarkedExcerpt(t *testing.T) {
	db, dir := built(t)
	r := do(t, "-db", db, "search", "-index", dir, "-json", "shallow")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var v struct {
		Hits []struct {
			Address string
			Excerpt string
		}
	}
	if err := json.Unmarshal([]byte(r.out), &v); err != nil || len(v.Hits) != 1 {
		t.Fatalf("json %q: %v", r.out, err)
	}
	if !strings.Contains(v.Hits[0].Excerpt, "<mark>shallow</mark> water") {
		t.Errorf("excerpt = %q", v.Hits[0].Excerpt)
	}
}
