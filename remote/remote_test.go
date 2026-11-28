package remote_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/cmarkfind/remote"
	"github.com/rsdoiel/markdownweb/fetch"
)

// Phase 7, slice 1: an index read over HTTP and a mirror kept on disk. The
// server is real (httptest) and serves an index the package builds; the hostile
// cases are a server that lies (workspace DR-0007: through the path the data
// takes in use).

var built = time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

func docs() []index.Doc {
	return []index.Doc{
		{Address: "https://example.org/b.md", Title: "Gardening notes", Description: "Soil and seeds", Body: "# Soil\n\nTomatoes like warm soil. Seeds go in early."},
		{Address: "https://example.org/a.md", Title: "Search engines", Description: "How a search index works", Body: "An index maps words to pages. Tomatoes are not mentioned here, only a garden of words."},
		{Address: "https://example.org/c.md", Title: "Untitled", Body: "Nothing about the topic. The quick brown fox."},
	}
}

// buildIndex writes an index of generation gen (and the extra documents) to a new directory.
func buildIndex(t *testing.T, gen uint64, extra ...index.Doc) string {
	t.Helper()
	dir := t.TempDir()
	if err := index.Build(dir, append(docs(), extra...), index.BuildOptions{Generation: gen, Built: built}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// server serves a directory under /cmarkfind/ and records every request path.
// over replaces the answer for a path (relative to /cmarkfind/) when it returns true.
type server struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []string
	over func(w http.ResponseWriter, r *http.Request, rel string) bool
}

func serve(t *testing.T, dir string) *server {
	t.Helper()
	s := &server{}
	files := http.StripPrefix("/cmarkfind/", http.FileServer(http.Dir(dir)))
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.URL.RequestURI())
		s.mu.Unlock()
		rel := strings.TrimPrefix(r.URL.Path, "/cmarkfind/")
		if s.over != nil && s.over(w, r, rel) {
			return
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) manifest() string { return s.URL + "/cmarkfind/manifest.json" }

func (s *server) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func fetcher() *fetch.Fetcher { return fetch.New(fetch.Options{UserAgent: "cmarkfind-test"}) }

// firstShard is the relative path of some file of the index that is not the manifest.
func firstShard(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "manifest.json" {
			rel, _ := filepath.Rel(dir, p)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(names) == 0 {
		t.Fatal("the index has no shard")
	}
	sort.Strings(names)
	return names[0]
}

func search(t *testing.T, ix *index.Index, q string) []index.Hit {
	t.Helper()
	n, err := index.ParseQuery(q, nil)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search(n, 10)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return hits
}

// listing is every file under dir with its bytes, for "nothing changed" checks.
func listing(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			m[rel] = string(b)
		}
		return nil
	})
	return m
}

func mustOpen(t *testing.T, s *server) *index.Index {
	t.Helper()
	src, err := remote.NewSource(context.Background(), fetcher(), s.manifest())
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.OpenSource(src)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

// ---- the source -----------------------------------------------------------

func TestASourceSearchesAnIndexOverHTTPAndAsksOnlyForItsOwnFiles(t *testing.T) {
	s := serve(t, buildIndex(t, 1))
	ix := mustOpen(t, s)
	if hits := search(t, ix, "tomatoes"); len(hits) != 2 {
		t.Fatalf("%d hits, want 2", len(hits))
	}
	if err := index.VerifySource(mustSource(t, s)); err != nil {
		t.Fatalf("verify over HTTP: %v", err)
	}
	for _, r := range s.requests() {
		if !strings.HasPrefix(r, "/cmarkfind/") || strings.Contains(r, "..") || strings.Contains(r, "?") {
			t.Errorf("request %q is not a plain file of the index", r)
		}
	}
}

func mustSource(t *testing.T, s *server) *remote.Source {
	t.Helper()
	src, err := remote.NewSource(context.Background(), fetcher(), s.manifest())
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// Only a name the format makes (kind/digest.cbor[.gz], or manifest.json) is
// ever turned into an address; anything else is refused before a request.
func TestASourceRefusesNamesOutsideTheIndexWithoutAskingTheServer(t *testing.T) {
	s := serve(t, buildIndex(t, 1))
	src := mustSource(t, s)
	before := len(s.requests())
	for _, name := range []string{
		"../x", "/etc/passwd", "https://evil.example/x", "//evil.example/x", "manifest.json?x=1", "manifest.json#a",
		"a/b/c.cbor", `a\b.cbor`, "chunks/../../x.cbor", ".", "", "chunks/%2e%2e/x", "chunks/x y.cbor",
	} {
		if _, err := src.Open(name); err == nil {
			t.Errorf("Open(%q) succeeded", name)
		}
	}
	if n := len(s.requests()) - before; n != 0 {
		t.Errorf("%d requests for names the format never makes", n)
	}
}

func TestAManifestThatNamesADigestWithAPathMakesNoRequestForIt(t *testing.T) {
	dir := buildIndex(t, 1)
	rewriteManifest(t, dir, func(m map[string]any) {
		chunks := m["chunks"].([]any)
		chunks[0].([]any)[2] = []any{"../../secret", 10}
	})
	s := serve(t, dir)
	_, err := openAndVerify(t, s)
	if !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error", err)
	}
	for _, r := range s.requests() {
		if strings.Contains(r, "secret") || strings.Contains(r, "..") {
			t.Errorf("asked the server for %q", r)
		}
	}
}

func TestAnIndexRequiringAFeatureThisReaderLacksIsADataError(t *testing.T) {
	dir := buildIndex(t, 1)
	rewriteManifest(t, dir, func(m map[string]any) { m["required"] = []any{"future-feature/9"} })
	if _, err := openAndVerify(t, serve(t, dir)); !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error", err)
	}
}

func TestACrossOriginRedirectIsRefusedAndTheOtherOriginIsNeverAsked(t *testing.T) {
	good := serve(t, buildIndex(t, 1))
	other := serve(t, buildIndex(t, 1)) // a second origin holding a perfectly good index
	hostile := serve(t, buildIndex(t, 1))
	hostile.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		if rel == "manifest.json" {
			http.Redirect(w, r, other.URL+"/cmarkfind/manifest.json", http.StatusFound)
			return true
		}
		return false
	}
	_ = good
	if _, err := openAndVerify(t, hostile); !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error for a cross-origin redirect", err)
	}
	if n := len(other.requests()); n != 0 {
		t.Errorf("the other origin was asked %d times: %v", n, other.requests())
	}
}

func TestASameOriginRedirectWithinTheLimitIsFollowed(t *testing.T) {
	s := serve(t, buildIndex(t, 1))
	s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		if rel == "manifest.json" && r.URL.Query().Get("hop") == "" {
			http.Redirect(w, r, "/cmarkfind/manifest.json?hop=1", http.StatusFound)
			return true
		}
		return false
	}
	// the redirect target is the manifest again; the index must still open
	if _, err := openAndVerify(t, s); err != nil {
		t.Fatalf("a same-origin redirect was refused: %v", err)
	}
}

func TestARedirectLoopStopsAndIsUnavailable(t *testing.T) {
	s := serve(t, buildIndex(t, 1))
	s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		if rel == "manifest.json" {
			http.Redirect(w, r, "/cmarkfind/manifest.json", http.StatusFound)
			return true
		}
		return false
	}
	_, err := openAndVerify(t, s)
	if !errors.Is(err, fetch.ErrUnavailable) {
		t.Fatalf("err %v, want unavailable", err)
	}
	if n := len(s.requests()); n > 10 {
		t.Errorf("%d requests: the redirect limit did not stop it", n)
	}
}

func TestAMissingManifestIsMissingAndAServerErrorOrNoServerIsUnavailable(t *testing.T) {
	s := serve(t, t.TempDir()) // an empty directory: 404
	if _, err := openAndVerify(t, s); !index.IsMissing(err) {
		t.Errorf("404: err %v, want missing", err)
	}
	s2 := serve(t, buildIndex(t, 1))
	s2.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		http.Error(w, "boom", http.StatusInternalServerError)
		return true
	}
	if _, err := openAndVerify(t, s2); !errors.Is(err, fetch.ErrUnavailable) {
		t.Errorf("500: err %v, want unavailable", err)
	}
	s3 := serve(t, buildIndex(t, 1))
	url := s3.manifest()
	s3.Close()
	src, err := remote.NewSource(context.Background(), fetcher(), url)
	if err == nil {
		_, err = index.OpenSource(src)
	}
	if !errors.Is(err, fetch.ErrUnavailable) {
		t.Errorf("no server: err %v, want unavailable", err)
	}
}

func TestAManifestOverTheCapIsADataErrorWhateverTheTransportCap(t *testing.T) {
	for _, size := range []int{5 << 20, 9 << 20} { // over the index cap; over the fetcher's body cap
		s := serve(t, buildIndex(t, 1))
		s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
			if rel == "manifest.json" {
				w.Write([]byte(`{"format":"cmarkfind/1"` + strings.Repeat(" ", size) + `}`))
				return true
			}
			return false
		}
		if _, err := openAndVerify(t, s); !index.IsDataError(err) {
			t.Errorf("manifest of %d bytes: err %v, want a data error", size, err)
		}
	}
}

func TestAShardWithTheWrongBytesIsADataErrorAtSearchTimeNotAPanic(t *testing.T) {
	dir := buildIndex(t, 1)
	shard := firstShard(t, dir)
	s := serve(t, dir)
	s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		if rel == shard {
			w.Write(bytes.Repeat([]byte{0x42}, 64))
			return true
		}
		return false
	}
	_, err := openAndVerify(t, s)
	if !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error", err)
	}
}

func TestACompressionBombServedAsAShardIsADataError(t *testing.T) {
	dir := buildIndex(t, 1)
	shard := firstShard(t, dir)
	s := serve(t, dir)
	s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
		if rel != shard {
			return false
		}
		zw := gzip.NewWriter(w)
		zero := make([]byte, 1<<20)
		for i := 0; i < 400; i++ { // 400 MiB of zeros, a few hundred KiB on the wire
			if _, err := zw.Write(zero); err != nil {
				return true
			}
		}
		zw.Close()
		return true
	}
	if _, err := openAndVerify(t, s); !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error", err)
	}
}

// openAndVerify opens the source, then reads every file of the index through it.
func openAndVerify(t *testing.T, s *server) (*index.Index, error) {
	t.Helper()
	src, err := remote.NewSource(context.Background(), fetcher(), s.manifest())
	if err != nil {
		return nil, err
	}
	if err := index.VerifySource(src); err != nil {
		return nil, err
	}
	return index.OpenSource(src)
}

func rewriteManifest(t *testing.T, dir string, edit func(map[string]any)) {
	t.Helper()
	p := filepath.Join(dir, "manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ = json.Marshal(m)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- the mirror -----------------------------------------------------------

func TestAMirrorStoresAVerifiedCopyThatOpensAndSearchesOffline(t *testing.T) {
	src := buildIndex(t, 3)
	s := serve(t, src)
	dir := filepath.Join(t.TempDir(), "mirror")
	rep, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Changed || rep.Generation != 3 || rep.Files < 2 {
		t.Errorf("report %+v", rep)
	}
	s.Close() // offline from here
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hits := search(t, ix, "tomatoes"); len(hits) != 2 {
		t.Errorf("%d hits offline, want 2", len(hits))
	}
	if err := index.Verify(dir); err != nil {
		t.Errorf("the mirror does not verify: %v", err)
	}
}

func TestMirroringTheSameGenerationChangesNothingAndFetchesNoShard(t *testing.T) {
	s := serve(t, buildIndex(t, 3))
	dir := filepath.Join(t.TempDir(), "mirror")
	if _, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir); err != nil {
		t.Fatal(err)
	}
	before, n := listing(t, dir), len(s.requests())
	rep, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changed || rep.Generation != 3 {
		t.Errorf("report %+v, want unchanged at 3", rep)
	}
	if got := s.requests()[n:]; len(got) != 1 || !strings.HasSuffix(got[0], "/manifest.json") {
		t.Errorf("asked for %v, want only the manifest", got)
	}
	if after := listing(t, dir); !equal(before, after) {
		t.Error("the mirror's files changed")
	}
}

func TestAHigherGenerationReplacesTheMirrorWholeAndDropsTheOldFiles(t *testing.T) {
	old := buildIndex(t, 3)
	s := serve(t, old)
	dir := filepath.Join(t.TempDir(), "mirror")
	if _, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir); err != nil {
		t.Fatal(err)
	}
	newer := buildIndex(t, 4, index.Doc{Address: "https://example.org/d.md", Title: "Tomatoes again", Body: "Tomatoes, tomatoes."})
	s2 := serve(t, newer)
	rep, err := remote.Mirror(context.Background(), fetcher(), s2.manifest(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Changed || rep.Generation != 4 {
		t.Errorf("report %+v", rep)
	}
	if got, want := listing(t, dir), listing(t, newer); !equal(got, want) {
		t.Errorf("the mirror is not exactly the new index (%d files, want %d)", len(got), len(want))
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hits := search(t, ix, "tomatoes"); len(hits) != 3 {
		t.Errorf("%d hits, want 3", len(hits))
	}
}

func TestALowerGenerationIsRefusedAsARollbackAndTheMirrorIsKept(t *testing.T) {
	s := serve(t, buildIndex(t, 5))
	dir := filepath.Join(t.TempDir(), "mirror")
	if _, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir); err != nil {
		t.Fatal(err)
	}
	before := listing(t, dir)
	stale := serve(t, buildIndex(t, 4))
	_, err := remote.Mirror(context.Background(), fetcher(), stale.manifest(), dir)
	if !errors.Is(err, remote.ErrRollback) || !index.IsDataError(err) {
		t.Fatalf("err %v, want a rollback (a data error)", err)
	}
	if !equal(before, listing(t, dir)) {
		t.Error("a refused rollback changed the mirror")
	}
	for _, r := range stale.requests() {
		if !strings.HasSuffix(r, "/manifest.json") {
			t.Errorf("a stale index was fetched past its manifest: %s", r)
		}
	}
}

func TestAFailedMirrorStoresNothingAndKeepsTheOldCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mirror")
	good := serve(t, buildIndex(t, 1))
	if _, err := remote.Mirror(context.Background(), fetcher(), good.manifest(), dir); err != nil {
		t.Fatal(err)
	}
	before := listing(t, dir)
	parent := filepath.Dir(dir)
	entries := func() int { e, _ := os.ReadDir(parent); return len(e) }
	nBefore := entries()

	newer := buildIndex(t, 2)
	shard := firstShard(t, newer)
	for name, over := range map[string]func(http.ResponseWriter, *http.Request) bool{
		"server error": func(w http.ResponseWriter, r *http.Request) bool { http.Error(w, "boom", 500); return true },
		"wrong bytes":  func(w http.ResponseWriter, r *http.Request) bool { w.Write([]byte("lies")); return true },
	} {
		s := serve(t, newer)
		over := over
		s.over = func(w http.ResponseWriter, r *http.Request, rel string) bool {
			return rel == shard && over(w, r)
		}
		if _, err := remote.Mirror(context.Background(), fetcher(), s.manifest(), dir); err == nil {
			t.Errorf("%s: no error", name)
		}
		if !equal(before, listing(t, dir)) {
			t.Errorf("%s: the old copy changed", name)
		}
		if entries() != nBefore {
			t.Errorf("%s: a staging copy was left beside the mirror", name)
		}
	}
}

func TestAMirrorOfAnUnverifiableIndexStoresNothingNew(t *testing.T) {
	d := buildIndex(t, 1)
	rewriteManifest(t, d, func(m map[string]any) { m["required"] = []any{"future-feature/9"} })
	dir := filepath.Join(t.TempDir(), "mirror")
	_, err := remote.Mirror(context.Background(), fetcher(), serve(t, d).manifest(), dir)
	if !index.IsDataError(err) {
		t.Fatalf("err %v, want a data error", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the mirror directory was made: %v", err)
	}
}

// A mirror replaces a directory; it must be one it made.
func TestAMirrorWillNotReplaceADirectoryThatIsNotAnIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := listing(t, dir)
	_, err := remote.Mirror(context.Background(), fetcher(), serve(t, buildIndex(t, 1)).manifest(), dir)
	if err == nil {
		t.Fatal("the mirror replaced a directory that holds someone's files")
	}
	if !equal(before, listing(t, dir)) {
		t.Error("someone's files were changed")
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
