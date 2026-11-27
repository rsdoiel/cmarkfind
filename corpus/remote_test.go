package corpus

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/toc"
)

// webSite is a small Markdown site over HTTP: pages by path, an outline that
// lists the paths in outline, ETags from the content, and a count of requests
// by path and of the conditional headers seen.
type webSite struct {
	srv     *httptest.Server
	mu      sync.Mutex
	pages   map[string]string // path -> body; "status:NNN" as a body means answer that status
	outline []string          // paths the outline lists, in order
	ctype   map[string]string // path -> Content-Type, default text/markdown
	hits    map[string]int
	sawINM  map[string]string // path -> last If-None-Match
	robots  string
}

func newWebSite(t *testing.T, pages map[string]string) *webSite {
	t.Helper()
	w := &webSite{pages: pages, ctype: map[string]string{}, hits: map[string]int{}, sawINM: map[string]string{}}
	for p := range pages {
		w.outline = append(w.outline, p)
	}
	sortStrings(w.outline)
	w.srv = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	return w
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func (w *webSite) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hits[r.URL.Path]++
	switch r.URL.Path {
	case "/robots.txt":
		if w.robots == "" {
			http.NotFound(rw, r)
			return
		}
		fmt.Fprint(rw, w.robots)
		return
	case "/.well-known/toc.opml":
		rw.Header().Set("Content-Type", "text/x-opml")
		var b strings.Builder
		for _, p := range w.outline {
			fmt.Fprintf(&b, `<outline text="%s" url="http://%s%s"/>`, p, r.Host, p)
		}
		fmt.Fprintf(rw, `<?xml version="1.0"?><opml version="2.0"><head/><body>%s</body></opml>`, b.String())
		return
	}
	body, ok := w.pages[r.URL.Path]
	if !ok {
		http.NotFound(rw, r)
		return
	}
	if strings.HasPrefix(body, "status:") {
		var n int
		fmt.Sscanf(body, "status:%d", &n)
		rw.WriteHeader(n)
		return
	}
	w.sawINM[r.URL.Path] = r.Header.Get("If-None-Match")
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(body)))[:18] + `"`
	rw.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	ct := w.ctype[r.URL.Path]
	if ct == "" {
		ct = "text/markdown; charset=utf-8"
	}
	rw.Header().Set("Content-Type", ct)
	fmt.Fprint(rw, body)
}

func (w *webSite) set(path, body string) {
	w.mu.Lock()
	w.pages[path] = body
	w.mu.Unlock()
}
func (w *webSite) list(paths ...string) {
	w.mu.Lock()
	w.outline = paths
	w.mu.Unlock()
}
func (w *webSite) hit(path string) int { w.mu.Lock(); defer w.mu.Unlock(); return w.hits[path] }
func (w *webSite) inm(path string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sawINM[path]
}
func (w *webSite) toc() string { return w.srv.URL + "/.well-known/toc.opml" }

func remote(t *testing.T) map[string]string {
	return map[string]string{
		"/a.md": "---\ntitle: Otters\n---\nThe otter swims in the river.\n",
		"/b.md": "# Badgers\n\nBadgers dig setts in the woods.\n",
		"/c.md": "# Cranes\n\nA crane stands in the shallow water.\n",
	}
}

func fillRemote(t *testing.T, db *DB, w *webSite, o fetch.Options, lim toc.Limits) (*FillReport, error) {
	t.Helper()
	return db.FillRemote(context.Background(), fetch.New(o), w.toc(), lim)
}

func TestFillRemoteStoresTheListedPagesAsTierTocAndTheyAreSearchable(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	r, err := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if err != nil || r.Added != 3 || !r.Complete {
		t.Fatalf("report %+v err %v", r, err)
	}
	res := search(t, db, "badgers")
	if len(res.Hits) != 1 || res.Hits[0].Address != w.srv.URL+"/b.md" || res.Hits[0].Tier != "toc" || res.Hits[0].Site != w.toc() {
		t.Fatalf("hits %+v", res.Hits)
	}
	if res.Searched != 3 {
		t.Errorf("searched %d", res.Searched)
	}
}

// A refill asks with the ETag it kept; a 304 costs no body and writes nothing.
func TestARefillSendsTheETagAndAnUnchangedPageWritesNothing(t *testing.T) {
	db, path := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	before := fetchedAts(t, path)
	r, err := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if err != nil || r.Unchanged != 3 || r.Added+r.Updated+r.Gone+r.Failed != 0 {
		t.Fatalf("report %+v err %v", r, err)
	}
	if w.inm("/b.md") == "" {
		t.Error("the second read sent no If-None-Match")
	}
	if after := fetchedAts(t, path); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("fetched_at changed:\n%v\n%v", before, after)
	}
}

func TestAPageChangedOnTheServerIsUpdatedAndTheOldTextNoLongerMatches(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	w.set("/b.md", "# Badgers\n\nNow about weasels.\n")
	r, _ := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if r.Updated != 1 || r.Unchanged != 2 {
		t.Errorf("report %+v", r)
	}
	if len(search(t, db, "setts").Hits) != 0 || len(search(t, db, "weasels").Hits) != 1 {
		t.Error("the index did not follow the page")
	}
}

func TestAPageTheOutlineDropsOrTheServerNoLongerHasIsGone(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	w.list("/a.md", "/b.md") // c dropped from the outline
	delete(w.pages, "/b.md") // b listed but 404
	r, _ := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if r.Gone != 2 {
		t.Errorf("report %+v", r)
	}
	if len(search(t, db, "crane").Hits) != 0 || len(search(t, db, "badgers").Hits) != 0 {
		t.Error("gone pages were returned")
	}
	// back again: fetched without validators, searchable
	w.set("/c.md", remote(t)["/c.md"])
	w.list("/a.md", "/c.md")
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if len(search(t, db, "crane").Hits) != 1 {
		t.Error("a page listed again should come back")
	}
	if w.inm("/c.md") != "" {
		t.Errorf("a gone page was asked for conditionally: %q", w.inm("/c.md"))
	}
}

// A server error is not the page being gone: the stored copy stays.
func TestAServerErrorFailsTheFetchAndKeepsTheStoredPage(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	w.set("/b.md", "status:500")
	r, err := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if r.Failed != 1 || r.Gone != 0 || err == nil {
		t.Errorf("report %+v err %v", r, err)
	}
	if len(search(t, db, "badgers").Hits) != 1 {
		t.Error("the stored page should stay searchable")
	}
}

func TestRobotsTxtKeepsAPageOutAndIsCounted(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	w.robots = "User-agent: *\nDisallow: /b.md\n"
	r, err := fillRemote(t, db, w, fetch.Options{Robots: true}, toc.Limits{})
	if r.Added != 2 || r.Forbidden != 1 {
		t.Errorf("report %+v err %v", r, err)
	}
	if w.hit("/b.md") != 0 {
		t.Error("a disallowed page was requested")
	}
	if len(search(t, db, "badgers").Hits) != 0 {
		t.Error("a disallowed page was stored")
	}
}

func TestAPageThatIsNotMarkdownIsSkippedAndCounted(t *testing.T) {
	db, _ := open(t)
	pages := remote(t)
	pages["/page.html"] = "<html><body>hello</body></html>"
	w := newWebSite(t, pages)
	w.ctype["/page.html"] = "text/html"
	r, _ := fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	if r.Added != 3 || r.Skipped != 1 {
		t.Errorf("report %+v", r)
	}
}

func TestACutOutlineMarksNothingGone(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	r, _ := fillRemote(t, db, w, fetch.Options{}, toc.Limits{Pages: 1})
	if r.Complete || r.Gone != 0 {
		t.Errorf("report %+v", r)
	}
	if search(t, db, "river").Searched != 3 {
		t.Error("pages beyond the limit must stay")
	}
}

func TestACancelledFillReturnsTheErrorAndMarksNothingGone(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	fillRemote(t, db, w, fetch.Options{}, toc.Limits{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.FillRemote(ctx, fetch.New(fetch.Options{}), w.toc(), toc.Limits{})
	if err == nil {
		t.Fatal("no error")
	}
	if search(t, db, "river").Searched != 3 {
		t.Error("a cancelled fill marked pages gone")
	}
}

func TestNoOutlineOverTheNetworkIsAnErrorTheCallerCanClassify(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, remote(t))
	_, err := db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.srv.URL+"/nowhere/toc.opml", toc.Limits{})
	if !errors.Is(err, toc.ErrNoOutline) {
		t.Errorf("err = %v, want toc.ErrNoOutline", err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	_, err = db.FillRemote(context.Background(), fetch.New(fetch.Options{}), dead.URL+"/toc.opml", toc.Limits{})
	if !errors.Is(err, fetch.ErrUnavailable) {
		t.Errorf("err = %v, want fetch.ErrUnavailable", err)
	}
}
