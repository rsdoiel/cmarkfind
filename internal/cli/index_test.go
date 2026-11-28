package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsdoiel/cmarkfind/index"
)

// built fills a corpus (the fill builds the index of its one set) and returns
// the corpus, the set's name and the folder of its index.
func built(t *testing.T) (db, set, dir string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	db = filled(t)
	set = setNames(t, db)[0]
	return db, set, setIndexDir(t, db, set)
}

func TestIndexBuildAndSearch(t *testing.T) {
	db, _, dir := built(t)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	r := do(t, "-db", db, "search", "otter")
	if r.code != 0 || !strings.Contains(r.out, "https://example.org/a.md") || strings.Contains(r.out, "b.md") {
		t.Errorf("otter: %+v", r)
	}
	if !strings.Contains(r.out, "Otters") {
		t.Errorf("title missing: %q", r.out)
	}
	if r := do(t, "-db", db, "search", `"shallow water" OR badgers`); r.code != 0 ||
		!strings.Contains(r.out, "c.md") || !strings.Contains(r.out, "b.md") || strings.Contains(r.out, "a.md") {
		t.Errorf("boolean: %+v", r)
	}
	if r := do(t, "-db", db, "search", "title:crane*"); r.code != 0 || !strings.Contains(r.out, "c.md") {
		t.Errorf("field prefix: %+v", r)
	}
}

func TestSearchJSONAndLimit(t *testing.T) {
	db, _, _ := built(t)
	r := do(t, "-db", db, "search", "-json", "-limit", "1", "the")
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

func TestSearchNoMatchIsOne(t *testing.T) {
	db, _, _ := built(t)
	if r := do(t, "-db", db, "search", "zebra"); r.code != 1 {
		t.Errorf("no match: %+v", r)
	}
	if r := do(t, "-db", db, "search", "--", "---"); r.code != 1 { // present, nothing searchable
		t.Errorf("punctuation: %+v", r)
	}
}

func TestSearchUsageErrorsAreTwo(t *testing.T) {
	db, _, _ := built(t)
	for name, args := range map[string][]string{
		"no query":  {"search"},
		"too long":  {"search", strings.Repeat("a ", 600)},
		"too many":  {"search", strings.Repeat("a ", 70)},
		"bad limit": {"search", "-limit", "0", "a"},
	} {
		r := do(t, append([]string{"-db", db}, args...)...)
		if r.code != 2 {
			t.Errorf("%s: exit %d, %q", name, r.code, r.errOut)
		}
	}
}

func TestIndexMissingIsSixtySix(t *testing.T) {
	db, set, dir := built(t)
	os.RemoveAll(dir)
	if r := do(t, "-db", db, "indexer", "validate", set); r.code != 66 {
		t.Errorf("validate a set with no index: exit %d, %q", r.code, r.errOut)
	}
	// list says so and is not an error: a set with no index yet is a state
	if r := do(t, "-db", db, "indexer", "list", set); r.code != 0 || !strings.Contains(r.out, "not built") {
		t.Errorf("list: %+v", r)
	}
	// build needs the corpus
	if r := do(t, "-db", filepath.Join(t.TempDir(), "none.db"), "indexer", "build"); r.code != 66 {
		t.Errorf("build without corpus: %+v", r)
	}
}

func TestIndexDamagedIsSixtyFive(t *testing.T) {
	db, set, dir := built(t)
	// a data file changed after the build: validate refuses it
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
	if r := do(t, "-db", db, "indexer", "validate", set); r.code != 65 {
		t.Errorf("validate: %+v", r)
	}
	// a manifest that is not JSON
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{nope"), 0o644)
	for _, args := range [][]string{{"indexer", "validate", set}, {"indexer", "list", set}} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 65 {
			t.Errorf("%v: exit %d, %q", args, r.code, r.errOut)
		}
	}
}

func TestIndexValidateOK(t *testing.T) {
	db, set, _ := built(t)
	for _, args := range [][]string{{"indexer", "validate", set}, {"indexer", "validate"}} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 0 {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestIndexListSaysWhatIsThere(t *testing.T) {
	db, set, _ := built(t)
	for _, args := range [][]string{{"indexer", "list", set}, {"indexer"}} { // a bare indexer lists every set
		r := do(t, append([]string{"-db", db}, args...)...)
		if r.code != 0 || !strings.Contains(r.out, "pages 3") || !strings.Contains(r.out, "generation 1") {
			t.Errorf("%v: %+v", args, r)
		}
	}
	r := do(t, "-db", db, "indexer", "list", "-json")
	var v struct {
		Indexes []map[string]any
	}
	if json.Unmarshal([]byte(r.out), &v) != nil || len(v.Indexes) != 1 || v.Indexes[0]["pages"] != float64(3) || v.Indexes[0]["generation"] != float64(1) {
		t.Errorf("json: %q", r.out)
	}
}

func TestIndexDirectoryIsBesideTheCorpusAndFollowsTheEnvironment(t *testing.T) {
	db := filled(t)
	t.Setenv("CMARKFIND_INDEX", "")
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	dir := setIndexDir(t, db, setNames(t, db)[0])
	if !strings.HasPrefix(dir, filepath.Join(filepath.Dir(db), "index", "sets")+string(filepath.Separator)) {
		t.Errorf("the index is at %s, not beside the corpus", dir)
	}
	// the environment names another folder; the set's index is built there on first use
	other := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("CMARKFIND_INDEX", other)
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 || !strings.Contains(r.out, "built 1") {
		t.Fatalf("%+v", r)
	}
	if d := setIndexDir(t, db, setNames(t, db)[0]); !strings.HasPrefix(d, other) {
		t.Errorf("CMARKFIND_INDEX not used: %s", d)
	}
}

func TestIndexRebuildRaisesGenerationAndPrunes(t *testing.T) {
	db, set, dir := built(t)
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
	change := func(body, hash string) { // a page changes: its text and its hash
		raw, _ := openRaw(db)
		raw.Exec(`UPDATE pages SET body = ?, hash = ? WHERE address LIKE '%a.md'`, body, hash)
		raw.Close()
	}
	change("The otter swims and dives.", "h2")
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 || !strings.Contains(r.out, "built 1") {
		t.Fatalf("%+v", r)
	}
	if r := do(t, "-db", db, "indexer", "list", set); !strings.Contains(r.out, "generation 2") {
		t.Errorf("%+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate", set); r.code != 0 {
		t.Errorf("verify after rebuild: %+v", r)
	}
	// the previous generation's files stay for a while, for a search still reading them
	if after := count(); after <= before {
		t.Errorf("the old files were pruned at once: %d files before, %d after", before, after)
	}
	old := time.Now().Add(-time.Hour)
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
	change("The otter swims and dives and rests.", "h3")
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if after := count(); after > before {
		t.Errorf("old files were left: %d files before, %d after", before, after)
	}
	if r := do(t, "-db", db, "search", "dives"); r.code != 0 {
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
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, w := range []string{"badgers", "crane"} {
		if r := do(t, "-db", db, "search", w); r.code != 1 {
			t.Errorf("%s still indexed: %+v", w, r)
		}
	}
	if r := do(t, "-db", db, "search", "otter"); r.code != 0 {
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
	db, set, _ := built(t)
	for name, args := range map[string][]string{
		"two sets":        {"indexer", "build", set, set},
		"bad chunk":       {"indexer", "build", "-chunk", "0", set},
		"unknown verb":    {"indexer", "frob"},
		"validate 2 sets": {"indexer", "validate", set, set},
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
	t.Setenv("CMARKFIND_INDEX", filepath.Join(f, "sub")) // a directory under a file
	if r := do(t, "-db", db, "indexer", "build"); r.code != 73 {
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

// The switch: search reads the index that the fill built, with no indexer
// build in between, and keeps its output and exit codes.
func TestSearchAfterAFillNeedsNoIndexerBuild(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db := filled(t)
	r := do(t, "-db", db, "search", "badgers")
	if r.code != 0 || !strings.Contains(r.out, "https://example.org/b.md") || !strings.Contains(r.out, "[Badgers]") && !strings.Contains(r.out, "[badgers]") {
		t.Errorf("%+v", r)
	}
	if !strings.Contains(r.errOut, "1 hit, searched 3 pages") {
		t.Errorf("count line: %q", r.errOut)
	}
	if r := do(t, "-db", db, "search", "zzzqx"); r.code != 1 {
		t.Errorf("no match: %+v", r)
	}
}

func TestSearchOfAFileUpgradedFromFTS5BuildsTheIndexOnFirstUse(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db := filled(t)
	os.RemoveAll(filepath.Join(filepath.Dir(db), "index"))
	if r := do(t, "-db", db, "search", "otter"); r.code != 0 || !strings.Contains(r.out, "a.md") {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(setIndexDir(t, db, setNames(t, db)[0]), "manifest.json")); err != nil {
		t.Error(err)
	}
}

func TestSearchQueryOverTheLimitIsUsageExit2InPlainAndJSON(t *testing.T) {
	db := filled(t)
	long := strings.Repeat("otter ", 20000)
	if r := do(t, "-db", db, "search", long); r.code != 2 {
		t.Errorf("%d: %.200s", r.code, r.errOut)
	}
	r := do(t, "-db", db, "search", "-json", long)
	if r.code != 2 || parseJSON(t, r.out)["class"] != "usage" {
		t.Errorf("%+v", r)
	}
}

func TestAnUnwritableIndexDirectoryIsCantCreateAfterAFill(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o644)
	t.Setenv("CMARKFIND_INDEX", filepath.Join(file, "idx")) // a directory under a file
	r := do(t, "-db", dbPath(t), "fill", localSite(t))
	if r.code != 73 {
		t.Errorf("%+v", r)
	}
}

// A folder named by CMARKFIND_INDEX that another corpus owns is a setting that
// is wrong, not a bug and not a missing file: exit 78 (config).
func TestAnIndexFolderAnotherCorpusOwnsIsExit78InPlainAndJSON(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	t.Setenv("CMARKFIND_INDEX", shared)
	filled(t) // the first corpus builds and owns it
	other := dbPath(t)
	r := do(t, "-db", other, "fill", localSite(t))
	if r.code != 78 || !strings.Contains(r.errOut, "another corpus") || !strings.Contains(r.errOut, shared) {
		t.Errorf("%+v", r)
	}
	r = do(t, "-db", other, "search", "-json", "otter")
	if r.code != 78 || parseJSON(t, r.out)["class"] != "config" {
		t.Errorf("%+v", r)
	}
}
