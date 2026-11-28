package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsdoiel/cmarkfind/index"
)

// Phase 7, slice 2 (cmarkfind DR-0017): indexer discover, add and refresh, and
// the mirror and reference sets they make, through cli.Run against a real
// server that publishes a real index.

var published = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

func publishedDocs(extra ...index.Doc) []index.Doc {
	return append([]index.Doc{
		{Address: "https://pub.example/b.md", Title: "Gardening notes", Description: "Soil and seeds", Body: "# Soil\n\nTomatoes like warm soil. Seeds go in early."},
		{Address: "https://pub.example/a.md", Title: "Search engines", Description: "How a search index works", Body: "An index maps words to pages. Tomatoes are not mentioned here, only a garden of words."},
		{Address: "https://pub.example/c.md", Title: "Untitled", Body: "Nothing about the topic. The quick brown fox."},
	}, extra...)
}

func buildPublished(t *testing.T, gen uint64, extra ...index.Doc) string {
	t.Helper()
	dir := t.TempDir()
	if err := index.Build(dir, publishedDocs(extra...), index.BuildOptions{Generation: gen, Built: published}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// site is a web site that publishes an index: the index folder under indexPath,
// and any other files by path (such as /.well-known/cmarkfind.json).
type site struct {
	*httptest.Server
	mu        sync.Mutex
	dir       string
	indexPath string // "/cmarkfind/" by default
	files     map[string]string
	reqs      []string
}

func publish(t *testing.T, dir string) *site {
	t.Helper()
	s := &site{dir: dir, indexPath: "/cmarkfind/", files: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.URL.Path)
		d, ip, body, ok := s.dir, s.indexPath, s.files[r.URL.Path], false
		_, ok = s.files[r.URL.Path]
		s.mu.Unlock()
		if ok {
			w.Write([]byte(body))
			return
		}
		if d != "" && strings.HasPrefix(r.URL.Path, ip) {
			http.StripPrefix(strings.TrimSuffix(ip, "/"), http.FileServer(http.Dir(d))).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *site) root() string       { return s.URL + "/" }
func (s *site) setName() string    { return s.URL + "/.well-known/toc.opml" }
func (s *site) set(dir string)     { s.mu.Lock(); s.dir = dir; s.mu.Unlock() }
func (s *site) put(p, body string) { s.mu.Lock(); s.files[p] = body; s.mu.Unlock() }
func (s *site) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func newDB(t *testing.T) string {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	return dbPath(t)
}

func sourceOf(t *testing.T, db, name string) string {
	t.Helper()
	r := do(t, "-db", db, "set", "list", "-json")
	for _, s := range parseJSON(t, r.out)["sets"].([]any) {
		m := s.(map[string]any)
		if m["name"] == name {
			return m["source"].(string)
		}
	}
	return ""
}

func hitsOf(t *testing.T, out string) []map[string]any {
	t.Helper()
	var hs []map[string]any
	for _, h := range parseJSON(t, out)["hits"].([]any) {
		hs = append(hs, h.(map[string]any))
	}
	return hs
}

// ---- discover -------------------------------------------------------------

func TestDiscoverFindsTheManifestAtTheDefaultPathAndChangesNothing(t *testing.T) {
	s := publish(t, buildPublished(t, 7))
	db := newDB(t)
	r := do(t, "-db", db, "indexer", "discover", "-json", s.root())
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	m := parseJSON(t, r.out)
	if m["manifest"] != s.URL+"/cmarkfind/manifest.json" || m["generation"] != float64(7) || m["pages"] != float64(3) {
		t.Errorf("discover said %s", r.out)
	}
	if _, err := os.Stat(db); err == nil {
		t.Error("discover made a corpus")
	}
	for _, p := range s.requests() {
		if strings.HasSuffix(p, ".cbor") || strings.HasSuffix(p, ".gz") {
			t.Errorf("discover fetched a shard: %s", p)
		}
	}
}

func TestDiscoverFollowsCmarkfindJSONAndNeverTriesTheDefaultPath(t *testing.T) {
	s := publish(t, buildPublished(t, 2))
	s.indexPath = "/search-index/"
	s.put("/.well-known/cmarkfind.json", `{"index": "/search-index/manifest.json", "other": 1}`)
	r := do(t, "-db", newDB(t), "indexer", "discover", "-json", s.root())
	if r.code != 0 || parseJSON(t, r.out)["manifest"] != s.URL+"/search-index/manifest.json" {
		t.Fatalf("%+v", r)
	}
	for _, p := range s.requests() {
		if strings.HasPrefix(p, "/cmarkfind/") {
			t.Errorf("the default path was tried although the site named another: %s", p)
		}
	}
}

func TestDiscoverTakesAManifestAddressDirectly(t *testing.T) {
	s := publish(t, buildPublished(t, 4))
	r := do(t, "-db", newDB(t), "indexer", "discover", "-json", s.URL+"/cmarkfind/manifest.json")
	if r.code != 0 || parseJSON(t, r.out)["generation"] != float64(4) {
		t.Fatalf("%+v", r)
	}
}

func TestDiscoverWithNoIndexAnywhereIsOneAndAFileNamingAMissingManifestIsOneWithoutAFallback(t *testing.T) {
	s := publish(t, "") // nothing published
	if r := do(t, "-db", newDB(t), "indexer", "discover", s.root()); r.code != 1 {
		t.Errorf("nothing published: %+v", r)
	}
	s2 := publish(t, buildPublished(t, 1)) // an index at the default path
	s2.put("/.well-known/cmarkfind.json", `{"index": "/elsewhere/manifest.json"}`)
	r := do(t, "-db", newDB(t), "indexer", "discover", s2.root())
	if r.code != 1 || !strings.Contains(r.errOut, "/elsewhere/manifest.json") {
		t.Errorf("a named, missing manifest: %+v", r)
	}
}

func TestDiscoverRefusesAHostileCmarkfindJSONWithExit65(t *testing.T) {
	good := publish(t, buildPublished(t, 1))
	for name, body := range map[string]string{
		"another origin":  `{"index": "https://evil.example/cmarkfind/manifest.json"}`,
		"another port":    `{"index": "` + good.URL + `x/cmarkfind/manifest.json"}`,
		"not a manifest":  `{"index": "/cmarkfind/secret.txt"}`,
		"a path climbing": `{"index": "/cmarkfind/../manifest.json"}`,
		"not a string":    `{"index": 7}`,
		"no index":        `{"x": 1}`,
		"duplicate keys":  `{"index": "/cmarkfind/manifest.json", "index": "/cmarkfind/manifest.json"}`,
		"not JSON":        `index = /cmarkfind/manifest.json`,
		"an array":        `["/cmarkfind/manifest.json"]`,
		"over 64 KiB":     `{"index": "/cmarkfind/manifest.json", "pad": "` + strings.Repeat("x", 70<<10) + `"}`,
		"a query":         `{"index": "/cmarkfind/manifest.json?x=1"}`,
		"a fragment":      `{"index": "/cmarkfind/manifest.json#x"}`,
		"a control char":  `{"index": "/cmarkfind/manifest.json\u0000"}`,
	} {
		s := publish(t, buildPublished(t, 1))
		s.put("/.well-known/cmarkfind.json", body)
		if r := do(t, "-db", newDB(t), "indexer", "discover", s.root()); r.code != 65 {
			t.Errorf("%s: exit %d, want 65: %+v", name, r.code, r)
		}
	}
}

func TestDiscoverExitCodes(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	url := s.root()
	s.Close()
	for name, tc := range map[string]struct {
		args []string
		want int
	}{
		"no argument":         {[]string{"indexer", "discover"}, 2},
		"two arguments":       {[]string{"indexer", "discover", "https://a.example/", "https://b.example/"}, 2},
		"not an http address": {[]string{"indexer", "discover", "ftp://a.example/"}, 2},
		"unreachable":         {[]string{"indexer", "discover", url}, 69},
	} {
		if r := do(t, append([]string{"-db", newDB(t)}, tc.args...)...); r.code != tc.want {
			t.Errorf("%s: exit %d, want %d: %+v", name, r.code, tc.want, r)
		}
	}
}

// ---- add ------------------------------------------------------------------

func TestAddMakesAMirrorThatSearchesOfflineWithTheIndexsOwnTitleAndTheTierMirror(t *testing.T) {
	s := publish(t, buildPublished(t, 3))
	db := newDB(t)
	if r := do(t, "-db", db, "indexer", "add", s.root()); r.code != 0 {
		t.Fatalf("add: %+v", r)
	}
	if got := sourceOf(t, db, s.setName()); got != "mirror" {
		t.Fatalf("the set %s has source %q, want mirror (sets: %v)", s.setName(), got, setNames(t, db))
	}
	s.Close() // offline from here
	r := do(t, "-db", db, "search", "-json", "tomatoes")
	if r.code != 0 {
		t.Fatalf("search: %+v", r)
	}
	hs := hitsOf(t, r.out)
	if len(hs) != 2 {
		t.Fatalf("%d hits, want 2: %s", len(hs), r.out)
	}
	for _, h := range hs {
		if h["tier"] != "mirror" || h["title"] == "" || h["site"] != s.setName() {
			t.Errorf("hit %v: want tier mirror, the index's title and the set as its site", h)
		}
	}
	if v := do(t, "-db", db, "indexer", "validate"); v.code != 0 {
		t.Errorf("validate: %+v", v)
	}
}

func TestAddReferenceKeepsOnlyTheManifestAndFetchesAtEachSearch(t *testing.T) {
	s := publish(t, buildPublished(t, 3))
	db := newDB(t)
	if r := do(t, "-db", db, "indexer", "add", "-reference", s.root()); r.code != 0 {
		t.Fatalf("add: %+v", r)
	}
	if got := sourceOf(t, db, s.setName()); got != "reference" {
		t.Fatalf("source %q, want reference", got)
	}
	var kept []string
	filepath.WalkDir(setIndexDir(t, db, s.setName()), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			kept = append(kept, d.Name())
		}
		return nil
	})
	for _, n := range kept {
		if n != "manifest.json" && n != "source.json" {
			t.Errorf("a reference kept %q: only the manifest and source.json belong there (%v)", n, kept)
		}
	}
	before := len(s.requests())
	r := do(t, "-db", db, "search", "-json", "tomatoes")
	if r.code != 0 || len(hitsOf(t, r.out)) != 2 || hitsOf(t, r.out)[0]["tier"] != "reference" {
		t.Fatalf("search: %+v", r)
	}
	if len(s.requests()) == before {
		t.Error("a reference search fetched nothing")
	}
}

func TestAddExitCodes(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	if r := do(t, "-db", db, "indexer", "add", s.root()); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := do(t, "-db", db, "indexer", "add", s.root()); r.code != 73 || !strings.Contains(r.errOut, s.setName()) {
		t.Errorf("the set exists: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "add", "-reference", s.root()); r.code != 73 {
		t.Errorf("the set exists, as a reference: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "add"); r.code != 2 {
		t.Errorf("no argument: %+v", r)
	}
	empty := publish(t, "")
	if r := do(t, "-db", newDB(t), "indexer", "add", empty.root()); r.code != 66 {
		t.Errorf("nothing published: %+v", r)
	}
	bad := buildPublished(t, 1)
	raw, _ := os.ReadFile(filepath.Join(bad, "manifest.json"))
	var man map[string]any
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	man["required"] = []any{"future/9"}
	raw, _ = json.Marshal(man)
	os.WriteFile(filepath.Join(bad, "manifest.json"), raw, 0o644)
	badSite := publish(t, bad)
	db2 := newDB(t)
	if r := do(t, "-db", db2, "indexer", "add", badSite.root()); r.code != 65 {
		t.Errorf("an unverifiable index: %+v", r)
	}
	if got := sourceOf(t, db2, badSite.setName()); got != "" {
		t.Errorf("a failed add left a set (%s)", got)
	}
	gone := publish(t, buildPublished(t, 1))
	url := gone.root()
	gone.Close()
	if r := do(t, "-db", newDB(t), "indexer", "add", url); r.code != 69 {
		t.Errorf("unreachable: %+v", r)
	}
}

func TestAddByManifestAddressNamesTheSetByTheManifest(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	m := s.URL + "/cmarkfind/manifest.json"
	if r := do(t, "-db", db, "indexer", "add", m); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := sourceOf(t, db, m); got != "mirror" {
		t.Errorf("sets %v, want one named %s", setNames(t, db), m)
	}
}

// A set is made by a fill from pages and by add from an index, never both.
func TestFillOfASiteThatIsAMirrorIsRefusedWithExitOneAndTheSetKeepsItsSource(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	if r := do(t, "-db", db, "indexer", "add", s.root()); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	r := do(t, "-db", db, "fill", s.URL+"/.well-known/toc.opml")
	if r.code != 1 || !strings.Contains(r.errOut, s.setName()) {
		t.Errorf("fill: %+v", r)
	}
	if got := sourceOf(t, db, s.setName()); got != "mirror" {
		t.Errorf("source %q", got)
	}
}

func TestSetFieldsOnAMirrorIsAUsageErrorAndForgetRemovesItWithItsFolder(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	if r := do(t, "-db", db, "set", "fields", s.setName(), "keywords=facet"); r.code != 2 {
		t.Errorf("set fields: %+v", r)
	}
	dir := setIndexDir(t, db, s.setName())
	if r := do(t, "-db", db, "forget", s.setName()); r.code != 0 {
		t.Fatalf("forget: %+v", r)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the mirror's folder is still there: %v", err)
	}
	if len(setNames(t, db)) != 0 {
		t.Errorf("sets after forget: %v", setNames(t, db))
	}
}

func TestSetIgnoreLeavesAMirrorOutOfSearchAndSearchSetStillReachesIt(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	do(t, "-db", db, "set", "ignore", s.setName())
	if r := do(t, "-db", db, "search", "tomatoes"); r.code != 1 {
		t.Errorf("search of an ignored mirror: %+v", r)
	}
	if r := do(t, "-db", db, "search-set", s.setName(), "tomatoes"); r.code != 0 {
		t.Errorf("search-set: %+v", r)
	}
}

// ---- curation by address --------------------------------------------------

func TestPinsTagsAndExclusionsApplyToMirrorHitsByAddress(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	first := func(args ...string) []map[string]any {
		r := do(t, append([]string{"-db", db, "search", "-json"}, args...)...)
		if r.code != 0 {
			t.Fatalf("search %v: %+v", args, r)
		}
		return hitsOf(t, r.out)
	}
	plain := first("tomatoes")
	if len(plain) != 2 {
		t.Fatalf("%d hits", len(plain))
	}
	last := plain[1]["address"].(string)
	// a pin puts the page first, and -pinned keeps only it
	if r := do(t, "-db", db, "pin", last); r.code != 0 {
		t.Fatalf("pin: %+v", r)
	}
	if got := first("tomatoes"); got[0]["address"] != last || got[0]["pinned"] != true {
		t.Errorf("pinned page not first: %v", got)
	}
	if got := first("-pinned", "tomatoes"); len(got) != 1 || got[0]["address"] != last {
		t.Errorf("-pinned: %v", got)
	}
	// a tag filters
	other := plain[0]["address"].(string)
	if r := do(t, "-db", db, "tag", other, "garden"); r.code != 0 {
		t.Fatalf("tag: %+v", r)
	}
	if got := first("-tag", "garden", "tomatoes"); len(got) != 1 || got[0]["address"] != other {
		t.Errorf("-tag: %v", got)
	}
	// an exclusion drops the hit at search time and leaves the index as published
	dir := setIndexDir(t, db, s.setName())
	before, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if r := do(t, "-db", db, "exclude", last); r.code != 0 {
		t.Fatalf("exclude: %+v", r)
	}
	if got := first("tomatoes"); len(got) != 1 || got[0]["address"] == last {
		t.Errorf("an excluded page is still a hit: %v", got)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "manifest.json")); string(after) != string(before) {
		t.Error("an exclusion changed the mirrored index")
	}
	if r := do(t, "-db", db, "unexclude", last); r.code != 0 {
		t.Fatalf("unexclude: %+v", r)
	}
	if got := first("tomatoes"); len(got) != 2 {
		t.Errorf("after unexclude: %v", got)
	}
}

// ---- refresh --------------------------------------------------------------

func TestRefreshHigherReplacesEqualChangesNothingAndLowerIsARollback(t *testing.T) {
	s := publish(t, buildPublished(t, 3))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	dir := setIndexDir(t, db, s.setName())
	snapshot := func() map[string]string {
		m := map[string]string{}
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() != "source.json" {
				b, _ := os.ReadFile(p)
				rel, _ := filepath.Rel(dir, p)
				m[rel] = string(b)
			}
			return nil
		})
		return m
	}
	same := func(a, b map[string]string) bool {
		if len(a) != len(b) {
			return false
		}
		for k, v := range a {
			if b[k] != v {
				return false
			}
		}
		return true
	}
	held := snapshot()

	// equal: nothing changes and no shard is fetched
	n := len(s.requests())
	if r := do(t, "-db", db, "indexer", "refresh", s.setName()); r.code != 0 {
		t.Fatalf("equal: %+v", r)
	}
	if !same(held, snapshot()) {
		t.Error("an equal generation changed the mirror")
	}
	for _, p := range s.requests()[n:] {
		if !strings.HasSuffix(p, "manifest.json") && p != "/.well-known/cmarkfind.json" {
			t.Errorf("an equal refresh asked for %s", p)
		}
	}

	// higher: replaced whole, and the new page is searchable
	s.set(buildPublished(t, 4, index.Doc{Address: "https://pub.example/d.md", Title: "More tomatoes", Body: "Tomatoes, tomatoes."}))
	if r := do(t, "-db", db, "indexer", "refresh", s.setName()); r.code != 0 {
		t.Fatalf("higher: %+v", r)
	}
	if r := do(t, "-db", db, "search", "-json", "tomatoes"); len(hitsOf(t, r.out)) != 3 {
		t.Errorf("after a higher generation: %s", r.out)
	}
	held = snapshot()

	// lower: refused, exit 65, nothing changed
	s.set(buildPublished(t, 2))
	r := do(t, "-db", db, "indexer", "refresh", s.setName())
	if r.code != 65 || !strings.Contains(r.errOut, s.setName()) {
		t.Errorf("lower: %+v", r)
	}
	if !same(held, snapshot()) {
		t.Error("a rollback changed the mirror")
	}
	if r := do(t, "-db", db, "search", "-json", "tomatoes"); len(hitsOf(t, r.out)) != 3 {
		t.Errorf("after a refused rollback: %s", r.out)
	}
}

func TestRefreshOfAReferenceStoresTheNewManifestAndRefusesALowerOne(t *testing.T) {
	s := publish(t, buildPublished(t, 3))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", "-reference", s.root())
	s.set(buildPublished(t, 5, index.Doc{Address: "https://pub.example/d.md", Title: "More tomatoes", Body: "Tomatoes, tomatoes."}))
	if r := do(t, "-db", db, "indexer", "refresh", s.setName()); r.code != 0 {
		t.Fatalf("higher: %+v", r)
	}
	if r := do(t, "-db", db, "search", "-json", "tomatoes"); len(hitsOf(t, r.out)) != 3 {
		t.Errorf("after refresh: %s", r.out)
	}
	s.set(buildPublished(t, 4))
	if r := do(t, "-db", db, "indexer", "refresh", s.setName()); r.code != 65 {
		t.Errorf("lower: %+v", r)
	}
}

func TestRefreshOfEverySetTriesAllAndExitsWithTheFirstFailure(t *testing.T) {
	good := publish(t, buildPublished(t, 1))
	dead := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", good.root())
	do(t, "-db", db, "indexer", "add", dead.root())
	good.set(buildPublished(t, 2, index.Doc{Address: "https://pub.example/d.md", Title: "More tomatoes", Body: "Tomatoes."}))
	dead.Close()
	r := do(t, "-db", db, "indexer", "refresh")
	if r.code != 69 || !strings.Contains(r.errOut, dead.setName()) {
		t.Errorf("refresh all: %+v", r)
	}
	// the healthy set was refreshed all the same
	if r := do(t, "-db", db, "search-set", good.setName(), "-json", "tomatoes"); len(hitsOf(t, r.out)) != 3 {
		t.Errorf("the healthy set was not refreshed: %s", r.out)
	}
}

func TestRefreshArgumentsAndExitCodes(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	do(t, "-db", db, "fill", localSite(t))
	local := ""
	for _, n := range setNames(t, db) {
		if sourceOf(t, db, n) == "local" {
			local = n
		}
	}
	if local == "" {
		t.Fatalf("no local set among %v", setNames(t, db))
	}
	if r := do(t, "-db", db, "indexer", "refresh", local); r.code != 2 {
		t.Errorf("a local set: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "refresh", "nothing-like-it"); r.code != 66 {
		t.Errorf("no such set: %+v", r)
	}
	if r := do(t, "-db", db, "indexer", "refresh", s.setName(), "extra"); r.code != 2 {
		t.Errorf("two arguments: %+v", r)
	}
	if r := do(t, "-db", newDB(t), "indexer", "refresh"); r.code != 66 {
		t.Errorf("no corpus: %+v", r)
	}
}

// ---- a set that cannot be read at search time -----------------------------

func TestAReferenceThatCannotBeReadIsLeftOutWithAWarningAndTheOthersAreSearched(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "fill", localSite(t))
	do(t, "-db", db, "indexer", "add", "-reference", s.root())
	s.Close()
	r := do(t, "-db", db, "search", "-json", "otter OR tomatoes")
	if r.code != 0 || len(hitsOf(t, r.out)) != 1 {
		t.Fatalf("search: %+v", r)
	}
	if !strings.Contains(r.errOut, s.setName()) {
		t.Errorf("no warning naming the set: %q", r.errOut)
	}
	if r := do(t, "-db", db, "search-set", s.setName(), "tomatoes"); r.code != 69 {
		t.Errorf("search-set of an unreachable reference: %+v", r)
	}
}

func TestSearchWhenEveryChosenSetFailedExitsWithTheFirstFailureClass(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", "-reference", s.root())
	s.Close()
	if r := do(t, "-db", db, "search", "tomatoes"); r.code != 69 {
		t.Errorf("only an unreachable reference: %+v", r)
	}
}

func TestAMirrorWhoseFilesWereDamagedOnDiskIsLeftOutWithExit65WhenAlone(t *testing.T) {
	s := publish(t, buildPublished(t, 1))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	dir := setIndexDir(t, db, s.setName())
	var shard string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "manifest.json" && d.Name() != "source.json" && shard == "" {
			shard = p
		}
		return nil
	})
	os.WriteFile(shard, []byte("damaged"), 0o644)
	if r := do(t, "-db", db, "search", "tomatoes"); r.code != 65 {
		t.Errorf("damaged mirror: %+v", r)
	}
}

// ---- what list shows ------------------------------------------------------

func TestIndexerListShowsTheSourceTheGenerationHeldAndWhenItWasChecked(t *testing.T) {
	s := publish(t, buildPublished(t, 6))
	db := newDB(t)
	do(t, "-db", db, "indexer", "add", s.root())
	r := do(t, "-db", db, "indexer", "list", "-json")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	rows := parseJSON(t, r.out)["indexes"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	m := rows[0].(map[string]any)
	if m["source"] != "mirror" || m["generation"] != float64(6) || m["checked_at"] == nil || m["checked_at"] == "" || m["manifest"] != s.URL+"/cmarkfind/manifest.json" {
		t.Errorf("row %v", m)
	}
	if txt := do(t, "-db", db, "indexer", "list"); !strings.Contains(txt.out, "mirror") || !strings.Contains(txt.out, "checked") {
		t.Errorf("text: %q", txt.out)
	}
}
