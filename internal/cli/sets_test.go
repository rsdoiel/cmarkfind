package cli

// Phase 5 tests (index sets): a set is one site instance, made by fill and
// removed by forget; search aggregates the sets that are not ignored and
// search-set searches one; each set has its own index folder and there is no
// corpus-wide index.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostSite writes pages and an outline that lists them under https://HOST/ and
// returns the folder, which is the site key of a local fill.
func hostSite(t *testing.T, host string, pages map[string]string) string {
	t.Helper()
	root := t.TempDir()
	var lines strings.Builder
	for name, body := range pages {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&lines, `<outline text="%s" url="https://%s/%s"/>`, name, host, name)
	}
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	opml := `<?xml version="1.0"?><opml version="2.0"><head/><body>` + lines.String() + `</body></opml>`
	if err := os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(opml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// smallPages is a set with one strong match for "otter"; bigPages has three
// matches of falling strength.
var smallPages = map[string]string{
	"a1.md": "# Otters\n\nOtter otter otter otter.\n",
}

var bigPages = map[string]string{
	"b1.md": "# Otter stories\n\nOtter otter otter otter otter.\n",
	"b2.md": "# River\n\nAn otter swam by in a long story about rivers and hills and fields.\n",
	"b3.md": "# Badgers\n\nBadgers dig setts, and an otter passed once in a very long wandering sentence about woods, hills, fields, rivers and weather.\n",
}

const (
	addrA1 = "https://a.example/a1.md"
	addrB1 = "https://b.example/b1.md"
	addrB2 = "https://b.example/b2.md"
	addrB3 = "https://b.example/b3.md"
)

// twoSets fills a small and a big site into one corpus and returns the corpus
// and the two site keys (the folders).
func twoSets(t *testing.T) (db, a, b string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	db = dbPath(t)
	a, b = hostSite(t, "a.example", smallPages), hostSite(t, "b.example", bigPages)
	for _, s := range []string{a, b} {
		if r := do(t, "-db", db, "fill", s); r.code != 0 {
			t.Fatalf("fill %s: %+v", s, r)
		}
	}
	return db, a, b
}

func setNames(t *testing.T, db string) []string {
	t.Helper()
	r := do(t, "-db", db, "set", "list", "-json")
	if r.code != 0 {
		t.Fatalf("set list: %+v", r)
	}
	var out []string
	sets, _ := parseJSON(t, r.out)["sets"].([]any)
	for _, s := range sets {
		out = append(out, s.(map[string]any)["name"].(string))
	}
	return out
}

// setIndexDir is the folder `set show` names for a set's index.
func setIndexDir(t *testing.T, db, name string) string {
	t.Helper()
	r := do(t, "-db", db, "set", "show", "-json", name)
	if r.code != 0 {
		t.Fatalf("set show %s: %+v", name, r)
	}
	dir, _ := parseJSON(t, r.out)["index"].(string)
	if dir == "" {
		t.Fatalf("set show has no index folder: %s", r.out)
	}
	return dir
}

func manifest(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("no manifest in %s: %v", dir, err)
	}
	return string(b)
}

// ---- sets are made by fill and removed by forget -------------------------------------

func TestAFillMakesOneSetPerSiteAndSetListShowsThem(t *testing.T) {
	db, a, b := twoSets(t)
	r := do(t, "-db", db, "set", "list")
	if r.code != 0 || !strings.Contains(r.out, a) || !strings.Contains(r.out, b) {
		t.Fatalf("set list: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "set", "list", "-json").out)
	sets, _ := j["sets"].([]any)
	if len(sets) != 2 {
		t.Fatalf("sets = %v", j["sets"])
	}
	byName := map[string]map[string]any{}
	for _, s := range sets {
		m := s.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if m := byName[a]; m == nil || m["pages"] != float64(1) || m["ignored"] != false || m["source"] != "local" {
		t.Errorf("set A = %v", byName[a])
	}
	if m := byName[b]; m == nil || m["pages"] != float64(3) {
		t.Errorf("set B = %v", byName[b])
	}
}

func TestSetListOfACorpusWithNoSetsIsExit0AndAnEmptyList(t *testing.T) {
	db, a, _ := twoSets(t)
	do(t, "-db", db, "forget", a)
	do(t, "-db", db, "forget", "--all")
	if r := do(t, "-db", db, "set", "list"); r.code != 0 {
		t.Errorf("empty list: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "set", "list", "-json").out)
	if sets, ok := j["sets"].([]any); !ok || len(sets) != 0 {
		t.Errorf("sets must be [], got %v", j["sets"])
	}
}

func TestForgetRemovesTheSetAndItsIndexFolder(t *testing.T) {
	db, a, b := twoSets(t)
	dirA := setIndexDir(t, db, a)
	if _, err := os.Stat(dirA); err != nil {
		t.Fatalf("set A has no index folder: %v", err)
	}
	if r := do(t, "-db", db, "forget", a); r.code != 0 {
		t.Fatalf("forget: %+v", r)
	}
	if got := setNames(t, db); len(got) != 1 || got[0] != b {
		t.Errorf("sets after forget = %v", got)
	}
	if _, err := os.Stat(dirA); !os.IsNotExist(err) {
		t.Errorf("the forgotten set's index folder is still there (%v)", err)
	}
	if r := do(t, "-db", db, "search", "otter"); r.code != 0 || strings.Contains(r.out, "a.example") {
		t.Errorf("search after forget: %+v", r)
	}
}

func TestThereIsNoCorpusWideIndexOnlyOneFolderPerSet(t *testing.T) {
	db, a, b := twoSets(t)
	top := filepath.Join(filepath.Dir(db), "index")
	if _, err := os.Stat(filepath.Join(top, "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("a corpus-wide index was built at %s (%v)", top, err)
	}
	ents, err := os.ReadDir(filepath.Join(top, "sets"))
	if err != nil || len(ents) != 2 {
		t.Fatalf("sets folder: %v, %d entries", err, len(ents))
	}
	for _, s := range []string{a, b} {
		if dir := setIndexDir(t, db, s); filepath.Dir(dir) != filepath.Join(top, "sets") {
			t.Errorf("index of %s is %s, not under %s/sets", s, dir, top)
		}
	}
}

func TestTheFirstSetBuildRemovesTheOldTopLevelIndexOfAFolderTheCorpusOwns(t *testing.T) {
	db, a, _ := twoSets(t)
	top := filepath.Join(filepath.Dir(db), "index")
	ents, _ := os.ReadDir(setIndexDir(t, db, a))
	for _, e := range ents { // an index of the old kind: copy a set's files to the top level
		if e.IsDir() {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(setIndexDir(t, db, a), e.Name()))
		os.WriteFile(filepath.Join(top, e.Name()), b, 0o644)
	}
	if _, err := os.Stat(filepath.Join(top, "manifest.json")); err != nil {
		t.Fatalf("the old index was not set up: %v", err)
	}
	if r := do(t, "-db", db, "indexer", "build"); r.code != 0 {
		t.Fatalf("indexer build: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(top, "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("the old top-level index is still there (%v)", err)
	}
	if r := do(t, "-db", db, "search", "otter"); r.code != 0 {
		t.Errorf("search after: %+v", r)
	}
}

// ---- naming a set ---------------------------------------------------------------------

func TestASetIsNamedByItsKeyItsHostOrItsAddress(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, srv := dbPath(t), webSite(t)
	if r := do(t, "-db", db, "fill", "-pause", "0s", srv.URL); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, name := range []string{
		srv.URL, srv.URL + "/", host, host + "/",
		srv.URL + "/.well-known/toc.opml",
	} {
		r := do(t, "-db", db, "set", "show", name)
		if r.code != 0 || !strings.Contains(r.out, host) {
			t.Errorf("set show %q: %+v", name, r)
		}
	}
}

func TestAFolderSetIsNamedByItsPath(t *testing.T) {
	db, a, _ := twoSets(t)
	if r := do(t, "-db", db, "set", "show", a); r.code != 0 {
		t.Errorf("set show %s: %+v", a, r)
	}
}

func TestAnUnknownSetIsExit66InPlainAndJSON(t *testing.T) {
	db, _, _ := twoSets(t)
	for _, args := range [][]string{
		{"set", "show", "nowhere.example"},
		{"set", "ignore", "nowhere.example"},
		{"set", "unignore", "nowhere.example"},
		{"set", "fields", "nowhere.example"},
		{"search-set", "nowhere.example", "otter"},
		{"indexer", "build", "nowhere.example"},
		{"indexer", "validate", "nowhere.example"},
	} {
		r := do(t, append([]string{"-db", db}, args...)...)
		if r.code != 66 {
			t.Errorf("%v: %+v", args, r)
		}
	}
	r := do(t, "-db", db, "set", "show", "-json", "nowhere.example")
	j := parseJSON(t, r.out)
	if r.code != 66 || j["class"] != "no_input" || j["code"] != float64(66) || j["error"] == "" {
		t.Errorf("json error: %+v", r)
	}
}

// Two sets that one name fits (the http and https of one host) are exit 2 and the
// message lists both. The pages are written straight into the corpus, since no
// web server here has both.
func TestAnAmbiguousSetNameIsExit2AndListsTheCandidates(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db := dbPath(t)
	do(t, "-db", db, "fill", hostSite(t, "x.example", smallPages)) // makes the file
	raw, err := openRaw(db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, s := range []string{"http://dual.test/.well-known/toc.opml", "https://dual.test/.well-known/toc.opml"} {
		_, err := raw.Exec(`INSERT INTO pages(address, site, title, body, hash) VALUES (?, ?, 'T', 'otter', ?)`,
			strings.Replace(s, "/.well-known/toc.opml", "/p.md", 1), s, s)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := do(t, "-db", db, "set", "show", "dual.test")
	if r.code != 2 || !strings.Contains(r.errOut, "http://dual.test") || !strings.Contains(r.errOut, "https://dual.test") {
		t.Errorf("ambiguous name: %+v", r)
	}
	if r := do(t, "-db", db, "set", "show", "https://dual.test"); r.code != 0 {
		t.Errorf("the scheme resolves it: %+v", r)
	}
}

func TestSetVerbsUsageErrorsAreExit2(t *testing.T) {
	db, a, _ := twoSets(t)
	for _, args := range [][]string{
		{"set"},
		{"set", "frobnicate"},
		{"set", "show"},
		{"set", "show", a, a},
		{"set", "ignore"},
		{"set", "unignore"},
		{"set", "fields"},
		{"set", "list", "extra"},
		{"set", "list", "-nonesuch"},
		{"search-set"},
		{"search-set", a},
		{"search", "-index", "somewhere", "otter"}, // the form is gone with the corpus-wide index
	} {
		r := do(t, append([]string{"-db", db}, args...)...)
		if r.code != 2 {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

// ---- searching: aggregate, one set, fusion ---------------------------------------------

func TestSearchAggregatesEverySetAndSearchSetLimitsToOne(t *testing.T) {
	db, a, b := twoSets(t)
	r := do(t, "-db", db, "search", "-limit", "20", "otter")
	for _, want := range []string{addrA1, addrB1, addrB2, addrB3} {
		if r.code != 0 || !strings.Contains(r.out, want) {
			t.Errorf("search is missing %s: %+v", want, r)
		}
	}
	r = do(t, "-db", db, "search-set", a, "otter")
	if r.code != 0 || !strings.Contains(r.out, addrA1) || strings.Contains(r.out, "b.example") {
		t.Errorf("search-set A: %+v", r)
	}
	r = do(t, "-db", db, "search-set", "-limit", "20", b, "otter")
	if r.code != 0 || strings.Contains(r.out, "a.example") || !strings.Contains(r.out, addrB3) {
		t.Errorf("search-set B: %+v", r)
	}
	if r := do(t, "-db", db, "search-set", a, "badgers"); r.code != 1 {
		t.Errorf("a query with no hit in that set is exit 1: %+v", r)
	}
}

func setHits(t *testing.T, db string, args ...string) []string {
	t.Helper()
	// -json goes right after the verb: options come before the arguments
	r := do(t, append([]string{"-db", db, args[0], "-json"}, args[1:]...)...)
	if r.code != 0 {
		t.Fatalf("%v: %+v", args, r)
	}
	var out []string
	hits, _ := parseJSON(t, r.out)["hits"].([]any)
	for _, h := range hits {
		out = append(out, h.(map[string]any)["address"].(string))
	}
	return out
}

// Reciprocal rank fusion: the best page of a small set is not buried under a big
// set's several matches. Both rank-one pages come before either rank-two page.
func TestReciprocalRankFusionPutsTheBestOfEachSetFirst(t *testing.T) {
	db, _, _ := twoSets(t)
	got := setHits(t, db, "search", "-limit", "20", "otter")
	if len(got) != 4 {
		t.Fatalf("hits = %v", got)
	}
	if !((got[0] == addrA1 && got[1] == addrB1) || (got[0] == addrB1 && got[1] == addrA1)) {
		t.Errorf("the two rank-one pages are not first: %v", got)
	}
	if !((got[2] == addrB2 && got[3] == addrB3) || (got[2] == addrB3 && got[3] == addrB2)) {
		t.Errorf("rank two and three of the big set are not last: %v", got)
	}
}

func TestOneSetKeepsItsOwnOrder(t *testing.T) {
	db, _, b := twoSets(t)
	one := setHits(t, db, "search-set", "-limit", "20", b, "otter")
	if len(one) != 3 || one[0] != addrB1 {
		t.Fatalf("set B alone = %v", one)
	}
	// ignore the small set: search over the one set that is left is that set's order
	if r := do(t, "-db", db, "set", "ignore", twoSetsA(t, db, b)); r.code != 0 {
		t.Fatalf("ignore: %+v", r)
	}
	if all := setHits(t, db, "search", "-limit", "20", "otter"); strings.Join(all, " ") != strings.Join(one, " ") {
		t.Errorf("search = %v, search-set = %v", all, one)
	}
}

// twoSetsA is the name of the set that is not b.
func twoSetsA(t *testing.T, db, b string) string {
	t.Helper()
	for _, n := range setNames(t, db) {
		if n != b {
			return n
		}
	}
	t.Fatal("no other set")
	return ""
}

func TestPinsTagsAndExclusionsAreCorpusWideAndApplyInEverySet(t *testing.T) {
	db, a, b := twoSets(t)
	if r := do(t, "-db", db, "pin", addrB3); r.code != 0 {
		t.Fatalf("pin: %+v", r)
	}
	got := setHits(t, db, "search", "-limit", "20", "otter")
	if len(got) == 0 || got[0] != addrB3 {
		t.Errorf("the pinned page is not first: %v", got)
	}
	if r := do(t, "-db", db, "tag", addrA1, "keep"); r.code != 0 {
		t.Fatalf("tag: %+v", r)
	}
	if got := setHits(t, db, "search", "-tag", "keep", "otter"); len(got) != 1 || got[0] != addrA1 {
		t.Errorf("tag filter = %v", got)
	}
	if r := do(t, "-db", db, "exclude", "https://b.example/b3"); r.code != 0 {
		t.Fatalf("exclude: %+v", r)
	}
	for _, args := range [][]string{{"search", "-limit", "20", "otter"}, {"search-set", "-limit", "20", b, "otter"}} {
		if got := setHits(t, db, args...); strings.Contains(strings.Join(got, " "), addrB3) {
			t.Errorf("%v still finds the excluded page: %v", args, got)
		}
	}
	_ = a
}

// ---- ignoring a set --------------------------------------------------------------------

func TestAnIgnoredSetIsLeftOutOfSearchAndKeptBySearchSetAndShownByList(t *testing.T) {
	db, a, b := twoSets(t)
	if r := do(t, "-db", db, "set", "ignore", b); r.code != 0 {
		t.Fatalf("ignore: %+v", r)
	}
	got := setHits(t, db, "search", "-limit", "20", "otter")
	if len(got) != 1 || got[0] != addrA1 {
		t.Errorf("search with B ignored = %v", got)
	}
	if got := setHits(t, db, "search-set", "-limit", "20", b, "otter"); len(got) != 3 {
		t.Errorf("search-set on an ignored set = %v", got)
	}
	j := parseJSON(t, do(t, "-db", db, "set", "list", "-json").out)
	for _, s := range j["sets"].([]any) {
		m := s.(map[string]any)
		if want := m["name"] == b; m["ignored"] != want {
			t.Errorf("%v ignored = %v, want %v", m["name"], m["ignored"], want)
		}
	}
	if r := do(t, "-db", db, "set", "list"); !strings.Contains(r.out, "ignored") {
		t.Errorf("plain set list does not say which is ignored: %q", r.out)
	}
	if r := do(t, "-db", db, "set", "unignore", b); r.code != 0 {
		t.Fatalf("unignore: %+v", r)
	}
	if got := setHits(t, db, "search", "-limit", "20", "otter"); len(got) != 4 {
		t.Errorf("search after unignore = %v", got)
	}
	_ = a
}

func TestWhenEverySetIsIgnoredSearchSaysSoAndExits1(t *testing.T) {
	db, a, b := twoSets(t)
	do(t, "-db", db, "set", "ignore", a)
	do(t, "-db", db, "set", "ignore", b)
	r := do(t, "-db", db, "search", "otter")
	if r.code != 1 || !strings.Contains(r.errOut, "ignored") {
		t.Errorf("%+v", r)
	}
	if r := do(t, "-db", db, "search-set", a, "otter"); r.code != 0 {
		t.Errorf("search-set still works: %+v", r)
	}
}

func TestIgnoringTwiceAndUnignoringASetThatIsNotIgnoredAreFine(t *testing.T) {
	db, a, _ := twoSets(t)
	for _, v := range []string{"ignore", "ignore", "unignore", "unignore"} {
		if r := do(t, "-db", db, "set", v, a); r.code != 0 {
			t.Errorf("set %s: %+v", v, r)
		}
	}
}

func TestAnIgnoredSetSurvivesAFillOfItsSite(t *testing.T) {
	db, a, _ := twoSets(t)
	do(t, "-db", db, "set", "ignore", a)
	if r := do(t, "-db", db, "fill", a); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	for _, s := range parseJSON(t, do(t, "-db", db, "set", "list", "-json").out)["sets"].([]any) {
		if m := s.(map[string]any); m["name"] == a && m["ignored"] != true {
			t.Errorf("the fill cleared the ignore flag: %v", m)
		}
	}
}

// ---- the field mapping, until Phase 6 ---------------------------------------------------

func TestSetFieldsShowsTheEffectiveMappingOneFieldPerLine(t *testing.T) {
	db, a, _ := twoSets(t)
	r := do(t, "-db", db, "set", "fields", a)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(r.out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("a line is a field and its roles: %q", line)
		}
		got[f[0]] = f[1]
	}
	for field, want := range map[string]string{
		"title": "built-in", "body": "built-in", "keywords": "fielded", "author": "fielded", "datepublished": "date", "datecreated": "date",
	} {
		if got[field] != want {
			t.Errorf("%s = %q, want %q", field, got[field], want)
		}
	}
	if _, ok := got["tags"]; ok {
		t.Error("tags is not mapped by default")
	}
	j := parseJSON(t, do(t, "-db", db, "set", "fields", "-json", a).out)
	fields, _ := j["fields"].(map[string]any)
	if roles, _ := fields["author"].([]any); len(roles) != 1 || roles[0] != "fielded" {
		t.Errorf("json fields %v", j["fields"])
	}
	if j := parseJSON(t, do(t, "-db", db, "set", "show", "-json", a).out); j["fields"] != "default" {
		t.Errorf("set show fields = %v", j["fields"])
	}
}

func TestSetFieldsAssignsRolesRemovesAFieldAndResets(t *testing.T) {
	db, a, _ := twoSets(t)
	r := do(t, "-db", db, "set", "fields", a, "tags=facet,fielded", "keywords=none", "author=text")
	if r.code != 0 {
		t.Fatalf("assign: %+v", r)
	}
	out := do(t, "-db", db, "set", "fields", a).out
	for _, want := range []string{"tags facet,fielded", "author text"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Errorf("the mapping lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "keywords") {
		t.Errorf("keywords=none left it mapped:\n%s", out)
	}
	if j := parseJSON(t, do(t, "-db", db, "set", "show", "-json", a).out); j["fields"] != "custom" {
		t.Errorf("set show fields = %v", j["fields"])
	}
	if r := do(t, "-db", db, "set", "fields", "-reset", a); r.code != 0 || !strings.Contains(r.out, "keywords") {
		t.Errorf("reset: %+v", r)
	}
	if j := parseJSON(t, do(t, "-db", db, "set", "show", "-json", a).out); j["fields"] != "default" {
		t.Errorf("after reset fields = %v", j["fields"])
	}
}

func TestABadAssignmentIsExit2AndAnUnknownSetIs66(t *testing.T) {
	db, a, _ := twoSets(t)
	for _, args := range [][]string{
		{"set", "fields", a, "author=facets"},
		{"set", "fields", a, "title=facet"},
		{"set", "fields", a, "author"},
		{"set", "fields", a, "bad name=facet"},
		{"set", "fields", "-reset", a, "author=facet"},
	} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 2 {
			t.Errorf("%v: %+v", args, r)
		}
	}
	if r := do(t, "-db", db, "set", "fields", "nowhere.example", "author=facet"); r.code != 66 {
		t.Errorf("unknown set: %+v", r)
	}
	r := do(t, "-db", db, "set", "fields", "-json", a, "author=facets")
	if j := parseJSON(t, r.out); r.code != 2 || j["class"] != "usage" {
		t.Errorf("json error: %+v", r)
	}
}

// set fields builds the set it changed, as a fill does, so the next search
// reads the new mapping; a build after it has nothing to do.
func TestSetFieldsRebuildsThatSetAtOnceAndNoOther(t *testing.T) {
	db, a, b := twoSets(t)
	ma, mb := manifest(t, setIndexDir(t, db, a)), manifest(t, setIndexDir(t, db, b))
	if r := do(t, "-db", db, "set", "fields", a, "tags=facet"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if manifest(t, setIndexDir(t, db, a)) == ma {
		t.Error("the changed set's index was not replaced by set fields")
	}
	if manifest(t, setIndexDir(t, db, b)) != mb {
		t.Error("another set's index was replaced")
	}
	if r := do(t, "-db", db, "indexer", "build"); !strings.Contains(r.out, "built 0, unchanged 2") {
		t.Errorf("%+v", r)
	}
	// the same assignment again, and -reset to what it already is, change nothing
	ma = manifest(t, setIndexDir(t, db, a))
	do(t, "-db", db, "set", "fields", a, "tags=facet")
	if manifest(t, setIndexDir(t, db, a)) != ma {
		t.Error("an assignment that changed nothing rebuilt")
	}
}

func TestAnUnparsableDateIsAWarningOnFillAndTheExitIs0(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	root := hostSite(t, "d.example", map[string]string{
		"ok.md":  "---\ndate: 2026-11-27\n---\nzebra fine\n",
		"bad.md": "---\ndate: tomorrow-ish\n---\nzebra odd\n",
	})
	db := dbPath(t)
	r := do(t, "-db", db, "fill", root)
	if r.code != 0 || !strings.Contains(r.out, "added 2") {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.errOut, "warning:") || !strings.Contains(r.errOut, "https://d.example/bad.md") || !strings.Contains(r.errOut, "tomorrow-ish") || strings.Contains(r.errOut, "ok.md") {
		t.Errorf("warnings: %q", r.errOut)
	}
	if r := do(t, "-db", db, "search", "zebra"); r.code != 0 {
		t.Errorf("both pages are found by their text: %+v", r)
	}
	if r := do(t, "-db", db, "stats"); !strings.Contains(r.out, "schema 7") {
		t.Errorf("schema: %q", r.out)
	}
}

// ---- building: only what needs it --------------------------------------------------------

func TestAnUnchangedSetBuildsNothing(t *testing.T) {
	db, _, _ := twoSets(t)
	r := do(t, "-db", db, "indexer", "build")
	if r.code != 0 || !strings.Contains(r.out, "built 0, unchanged 2") {
		t.Errorf("%+v", r)
	}
}

func TestAFillRebuildsOnlyTheSetOfTheSiteThatChanged(t *testing.T) {
	db, a, b := twoSets(t)
	bBefore, aBefore := manifest(t, setIndexDir(t, db, b)), manifest(t, setIndexDir(t, db, a))
	if err := os.WriteFile(filepath.Join(a, "a1.md"), []byte("# Otters\n\nOtter otter otter otter and a heron.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := do(t, "-db", db, "fill", a); r.code != 0 || !strings.Contains(r.out, "updated 1") {
		t.Fatalf("fill: %+v", r)
	}
	if manifest(t, setIndexDir(t, db, a)) == aBefore {
		t.Error("the changed site's index was not rebuilt")
	}
	if manifest(t, setIndexDir(t, db, b)) != bBefore {
		t.Error("an unchanged site's index was rebuilt")
	}
	if r := do(t, "-db", db, "search", "heron"); r.code != 0 || !strings.Contains(r.out, addrA1) {
		t.Errorf("the new word is not found: %+v", r)
	}
}

func TestAFillThatChangesNothingBuildsNothing(t *testing.T) {
	db, a, b := twoSets(t)
	aBefore, bBefore := manifest(t, setIndexDir(t, db, a)), manifest(t, setIndexDir(t, db, b))
	do(t, "-db", db, "fill", a)
	if manifest(t, setIndexDir(t, db, a)) != aBefore || manifest(t, setIndexDir(t, db, b)) != bBefore {
		t.Error("an unchanged fill rebuilt an index")
	}
}

func TestAnExclusionRebuildsTheSetsItTouchesAndNoOther(t *testing.T) {
	db, a, b := twoSets(t)
	aBefore, bBefore := manifest(t, setIndexDir(t, db, a)), manifest(t, setIndexDir(t, db, b))
	if r := do(t, "-db", db, "exclude", "https://b.example/b3"); r.code != 0 {
		t.Fatalf("exclude: %+v", r)
	}
	if manifest(t, setIndexDir(t, db, b)) == bBefore {
		t.Error("the set the exclusion touches was not rebuilt")
	}
	if manifest(t, setIndexDir(t, db, a)) != aBefore {
		t.Error("a set the exclusion does not touch was rebuilt")
	}
}

func TestAMissingSetIndexIsBuiltOnTheNextSearch(t *testing.T) {
	db, a, _ := twoSets(t)
	dir := setIndexDir(t, db, a)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	r := do(t, "-db", db, "search", "otter")
	if r.code != 0 || !strings.Contains(r.out, addrA1) {
		t.Fatalf("search: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("the index was not rebuilt: %v", err)
	}
}

func TestIndexerBuildAndValidateTakeASetName(t *testing.T) {
	db, a, _ := twoSets(t)
	os.RemoveAll(setIndexDir(t, db, a))
	r := do(t, "-db", db, "indexer", "build", a)
	if r.code != 0 || !strings.Contains(r.out, "built 1, unchanged 0") {
		t.Errorf("build one set: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate", a); r.code != 0 {
		t.Errorf("validate one set: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate"); r.code != 0 {
		t.Errorf("validate every set: %+v", r)
	}
}

func TestADamagedSetIndexIsExit65AndTheOtherSetsStillValidate(t *testing.T) {
	db, a, b := twoSets(t)
	dir := setIndexDir(t, db, a)
	damaged := false
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !damaged && !e.IsDir() && strings.HasSuffix(p, ".cbor.gz") {
			os.WriteFile(p, []byte("damaged"), 0o644)
			damaged = true
		}
		return nil
	})
	if !damaged {
		t.Fatal("found no index file to damage")
	}
	if r := do(t, "-db", db, "indexer", "validate", a); r.code != 65 {
		t.Errorf("validate damaged: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "validate", b); r.code != 0 {
		t.Errorf("validate the other: %+v", r)
	}
}

// ---- documentation the registry tests enforce ---------------------------------------------

func TestTheSetVerbsAreInTheRegistryAndCompletion(t *testing.T) {
	r := do(t, "verbs", "-json")
	if r.code != 0 {
		t.Fatalf("verbs: %+v", r)
	}
	for _, want := range []string{`"set"`, `"search-set"`, `"ignore"`, `"unignore"`} {
		if !strings.Contains(r.out, want) {
			t.Errorf("verbs -json does not list %s", want)
		}
	}
	r = do(t, "completion", "bash")
	if r.code != 0 || !strings.Contains(r.out, "search-set") {
		t.Errorf("completion: code %d", r.code)
	}
}

func TestTheSetVerbsHaveHelpTopicsAndExitStatusText(t *testing.T) {
	for _, topic := range []string{"set", "search-set"} {
		if r := do(t, "help", topic); r.code != 0 || r.out == "" {
			t.Errorf("help %s: %+v", topic, r)
		}
	}
	r := do(t, "help", "exit-status")
	if r.code != 0 || !strings.Contains(r.out, "set") {
		t.Errorf("exit-status: %+v", r)
	}
}

// A set kept in an analyzer this program does not read is left out of the merge
// with a warning and is exit 65 when searched alone.
func TestASetInAnAnalyzerWeDoNotReadIsWarnedAboutAndSearchSetIsExit65(t *testing.T) {
	db, a, _ := twoSets(t)
	raw, err := openRaw(db)
	if err != nil {
		t.Fatal(err)
	}
	raw.Exec(`UPDATE sets SET source = 'mirror' WHERE site = ?`, a)
	raw.Close()
	mp := filepath.Join(setIndexDir(t, db, a), "manifest.json")
	b, _ := os.ReadFile(mp)
	os.WriteFile(mp, []byte(strings.Replace(string(b), `"unicode-word-v3"`, `"porter-stem-v1"`, 1)), 0o644)
	r := do(t, "-db", db, "search", "-limit", "20", "otter")
	if r.code != 0 || strings.Contains(r.out, "a.example") || !strings.Contains(r.out, addrB1) {
		t.Fatalf("search: %+v", r)
	}
	if !strings.Contains(r.errOut, "left out") || !strings.Contains(r.errOut, "porter-stem-v1") {
		t.Errorf("no warning: %q", r.errOut)
	}
	if r := do(t, "-db", db, "search-set", a, "otter"); r.code != 65 {
		t.Errorf("search-set: %+v", r)
	}
}
