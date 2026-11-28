package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsdoiel/cmarkfind/server"
)

// ---- hostile stored text -------------------------------------------------------------------------

const evilTitle = `<script>alert(1)</script>`
const evilBody = "hostile \"><img src=x onerror=alert(1)> [x](http://evil.example/) <b>bold</b> javascript:alert(1) \x07bell " // ends with a space

// hostileEnv is a corpus whose stored text is all attack: a title, a snippet, facet
// labels and values, a set whose name is markup, and a collection page whose
// address is not one a browser can open.
func hostileEnv(t *testing.T) *env {
	t.Helper()
	db := openCorpus(t)
	ctx := context.Background()
	parent := t.TempDir()
	root := filepath.Join(parent, `<img src=x onerror=alert(1)>`) // a set named by its folder
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	pages := map[string]string{
		"a.md": "---\ntitle: " + evilTitle + "\nkeywords: ['<img src=x onerror=1>', '[x](http://evil.example/)']\n---\n# " + evilTitle + "\n\n" + evilBody + strings.Repeat("w", 5000) + "\n",
		"b.md": "# Plain\n\n" + evilBody + "\n",
	}
	for n, b := range pages {
		os.WriteFile(filepath.Join(root, n), []byte(b), 0o644)
		lines.WriteString(`<outline text="` + n + `" url="https://hostile.example/` + n + `"/>`)
	}
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(`<opml version="2.0"><head/><body>`+lines.String()+`</body></opml>`), 0o644)
	if _, err := db.FillLocal(ctx, root, tocLimits()); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignFields(ctx, root, []string{`keywords=facet,fielded,"<i>Topics</i>"`}); err != nil {
		t.Fatal(err)
	}
	notes := t.TempDir()
	os.WriteFile(filepath.Join(notes, "hostile.md"), []byte("# Note\n\n"+evilBody+"\n"), 0o644)
	if _, err := db.FillCollection(ctx, "notes", notes, tocLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BuildSets(ctx, "", 0); err != nil {
		t.Fatal(err)
	}
	e := start(t, db, defaults())
	e.root = root
	return e
}

func TestHostileStoredTextMakesNothingExecutableOrLinkableInAnyFormat(t *testing.T) {
	e := hostileEnv(t)

	// JSON: valid, and the markup is only ever data
	for _, p := range []string{"/search.json?q=hostile", "/sets.json", "/stats.json"} {
		r := must(t, e.get(p), 200)
		if !json.Valid([]byte(r.body)) {
			t.Errorf("%s is not valid JSON", p)
		}
		if strings.ContainsAny(r.body, "\x00\x07") {
			t.Errorf("%s carries a control character", p)
		}
	}

	// HTML: no markup of the hostile text's own
	for _, p := range []string{"/search.html?q=hostile", "/sets.html", "/stats.html"} {
		body := must(t, e.get(p), 200).body
		for _, bad := range []string{"<script", "<img", "<b>", "<i>Topics", "\x07", "\x00"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
		for _, l := range linksOf(body) {
			lower := strings.ToLower(l)
			switch {
			case strings.HasPrefix(l, "?"), strings.HasPrefix(l, "/"), strings.HasPrefix(l, "https://hostile.example/"):
				// the server's own links (a facet's query carries the stored text as data) and the hits' addresses
			case strings.HasPrefix(lower, "javascript:"), strings.HasPrefix(lower, "data:"), strings.HasPrefix(lower, "collection:"), strings.Contains(lower, "evil.example"):
				t.Errorf("%s links %q", p, l)
			default:
				t.Errorf("%s links %q, which is not one of the page's own", p, l)
			}
		}
	}
	html := must(t, e.get("/search.html?q=hostile"), 200).body
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("the hostile title is not shown, escaped, as text:\n%.800s", html)
	}
	if !strings.Contains(html, "collection://notes/hostile.md") {
		t.Errorf("the collection page's address is not shown as text")
	}

	// Markdown: only links the server made or the hits' addresses
	for _, p := range []string{"/search?q=hostile", "/sets", "/stats"} {
		body := must(t, e.get(p), 200).body
		if strings.ContainsAny(body, "\x00\x07") {
			t.Errorf("%s carries a control character", p)
		}
		for _, l := range linksOf(body) {
			if strings.HasPrefix(l, "?") || strings.HasPrefix(l, "/") {
				continue // the server's own links
			}
			if strings.Contains(strings.ToLower(l), "evil.example") || strings.HasPrefix(strings.ToLower(l), "javascript:") {
				t.Errorf("%s links %q", p, l)
			}
		}
		if strings.Contains(body, "\n<script") || strings.Contains(body, "\n# <script") {
			t.Errorf("%s: a hostile line became markup", p)
		}
	}
	// a hostile set name is escaped where sets are listed
	if body := must(t, e.get("/sets.html"), 200).body; !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Errorf("the hostile set name is not escaped in /sets.html:\n%.600s", body)
	}
}

// ---- read-only and no building ----------------------------------------------------------------------

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, _ := d.Info()
		rel, _ := filepath.Rel(dir, p)
		if info != nil {
			m[rel] = info.ModTime().String() + " " + string(rune(info.Size()))
		}
		return nil
	})
	return m
}

func TestNoRequestWritesToTheIndexFolder(t *testing.T) {
	e := started(t)
	before := snapshot(t, e.db.IndexDir())
	for _, p := range []string{"/", "/search?q=tomatoes", "/search.json?q=tomatoes&startIndex=101", "/search.html?q=zebra", "/opensearch.xml", "/version", "/stats", "/sets", "/nothing", "/search?q=" + strings.Repeat("a", 1025)} {
		e.get(p)
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		e.do(m, "/search?q=tomatoes")
		e.do(m, "/sets")
	}
	if after := snapshot(t, e.db.IndexDir()); len(after) != len(before) {
		t.Errorf("the index folder changed: %d entries before, %d after", len(before), len(after))
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Errorf("%s changed", k)
			}
		}
	}
}

func TestASetThatNeedsAnIndexIsLeftOutWithAWarningAndNeverBuilt(t *testing.T) {
	db, _ := mainCorpus(t)
	other := writeSite(t, "b.example", map[string]string{"x.md": "# X\n\ntomatoes elsewhere\n", "y.md": "# Y\n\ntomatoes again\n"})
	if _, err := db.FillLocal(ctxT(t), other, tocLimits()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(db.SetIndexDir(other)); err != nil {
		t.Fatal(err)
	}
	var log lockedBuffer
	e := start(t, db, defaults(), func(s *server.Server) { s.Log = &log })
	j := jsonOf(t, must(t, e.get("/search.json?q=tomatoes&count=1"), 200))
	if j["totalResults"] != float64(250) {
		t.Errorf("total %v: the set with no index must be left out, not built", j["totalResults"])
	}
	if !strings.Contains(log.String(), other) {
		t.Errorf("no warning naming the set in the log: %q", log.String())
	}
	if _, err := os.Stat(db.SetIndexDir(other)); !os.IsNotExist(err) {
		t.Errorf("the server built an index: %v", err)
	}
	if r := e.get("/search.json?q=tomatoes&set=" + queryEscape(other)); r.status != 503 {
		t.Errorf("naming the set that needs building: %d, want 503\n%s", r.status, r.body)
	}
	for _, r := range []resp{e.get("/search?q=tomatoes"), e.get("/search.json?q=tomatoes")} {
		if strings.Contains(r.body, other) {
			t.Errorf("a warning reached a response body")
		}
	}
	sets := jsonOf(t, must(t, e.get("/sets.json"), 200))["sets"].([]any)
	for _, s := range sets {
		m := s.(map[string]any)
		if m["name"] == other && m["built"] != false {
			t.Errorf("/sets says the stale set is built: %v", m)
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// ---- limits ---------------------------------------------------------------------------------------------

func TestAnOverlongRequestLineOrHeaderIsRefused(t *testing.T) {
	e := started(t)
	long := "GET /search?q=" + strings.Repeat("a", 9<<10) + " HTTP/1.1\r\nHost: " + e.addr() + "\r\n\r\n"
	if got := e.raw(long).status; got != 414 {
		t.Errorf("a 9 KiB request line: %d, want 414", got)
	}
	big := "GET /search?q=a HTTP/1.1\r\nHost: " + e.addr() + "\r\nX-Big: " + strings.Repeat("b", 20<<10) + "\r\n\r\n"
	if got := e.raw(big).status; got != 431 && got != 0 {
		t.Errorf("a 20 KiB header: %d, want 431 or a closed connection", got)
	}
}

func TestSearchesBeyondTheConcurrencySettingGet503AndAreCounted(t *testing.T) {
	db, _ := mainCorpus(t)
	s := defaults()
	s.Concurrency = 2
	var inside int32
	release := make(chan struct{})
	e := start(t, db, s, func(sv *server.Server) {
		sv.BeforeSearch = func() { atomic.AddInt32(&inside, 1); <-release }
	})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.get("/search?q=tomatoes") }()
	}
	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&inside) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&inside) < 2 {
		close(release)
		t.Fatal("two searches never got in")
	}
	third := e.get("/search?q=tomatoes")
	if third.status != 503 || third.header.Get("Retry-After") != "1" {
		t.Errorf("the third search: %d, Retry-After %q, want 503 and 1", third.status, third.header.Get("Retry-After"))
	}
	// what is not a search is not held up
	for _, p := range []string{"/", "/version", "/opensearch.xml"} {
		if got := e.get(p).status; got != 200 {
			t.Errorf("%s while searches are busy: %d", p, got)
		}
	}
	st := jsonOf(t, must(t, e.get("/stats.json"), 200))
	if st["in_flight"] != float64(2) || st["busy"].(float64) < 1 {
		t.Errorf("stats while busy: in_flight %v, busy %v", st["in_flight"], st["busy"])
	}
	close(release)
	wg.Wait()
	if got := e.get("/search?q=tomatoes").status; got != 200 {
		t.Errorf("a search after the others finished: %d", got)
	}
}

func TestASlowClientIsCutOffByTheHeaderTimeout(t *testing.T) {
	db, _ := mainCorpus(t)
	e := start(t, db, defaults(), func(s *server.Server) { s.ReadHeaderTimeout = 300 * time.Millisecond })
	c, err := net.Dial("tcp", e.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("GET /search?q=x HTTP/1.1\r\nHost: " + e.addr() + "\r\n")) // the blank line never comes
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 512)
	start := time.Now()
	_, err = c.Read(buf)
	if err == nil {
		// a 408 and a close are both fine; waiting forever is not
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the connection was held for %v", time.Since(start))
	}
}
