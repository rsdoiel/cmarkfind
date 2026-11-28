package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/server"
	"github.com/rsdoiel/markdownweb/toc"
)

// Phase 8 (cmarkfind DR-0018): the server through a real listener on a free
// port, over a real corpus built in the test.

var testVersion = server.Version{Name: "cmarkfind", Version: "9.8.7", ReleaseDate: "2026-11-27", Hash: "abc1234"}

func defaults() server.Settings {
	return server.Settings{Port: 0, Concurrency: 4, Results: 100, Info: true}
}

// env is a running server over a corpus.
type env struct {
	t    *testing.T
	db   *corpus.DB
	srv  *server.Server
	ln   net.Listener
	port int
	root string // the folder of the main site
}

func (e *env) addr() string { return fmt.Sprintf("127.0.0.1:%d", e.port) }
func (e *env) base() string { return "http://" + e.addr() }

// writeSite writes pages and an outline that lists them by https://host/NAME.
func writeSite(t *testing.T, host string, pages map[string]string) string {
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
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(`<opml version="2.0"><head/><body>`+lines.String()+`</body></opml>`), 0o644)
	return root
}

// tomatoPages are n pages that say "tomatoes"; the even ones have the keyword
// go and the odd ones oberon, so a Topics facet has two values.
func tomatoPages(n int) map[string]string {
	m := map[string]string{}
	for i := 0; i < n; i++ {
		kw := "go"
		if i%2 == 1 {
			kw = "oberon"
		}
		m[fmt.Sprintf("p%03d.md", i)] = fmt.Sprintf("---\nkeywords: [%s]\n---\n# Page %d\n\ntomatoes grow on page %d\n", kw, i, i)
	}
	return m
}

func openCorpus(t *testing.T) *corpus.DB {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	db, err := corpus.Open(filepath.Join(t.TempDir(), "corpus.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mainCorpus is 250 tomato pages as one set, with a Topics facet on keywords.
func mainCorpus(t *testing.T) (*corpus.DB, string) {
	t.Helper()
	db := openCorpus(t)
	ctx := context.Background()
	root := writeSite(t, "a.example", tomatoPages(250))
	if _, err := db.FillLocal(ctx, root, toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignFields(ctx, root, []string{"keywords=facet,fielded,Topics"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BuildSets(ctx, "", 0); err != nil {
		t.Fatal(err)
	}
	return db, root
}

// start runs a server over db on a free loopback port until the test ends.
func start(t *testing.T, db *corpus.DB, s server.Settings, tune ...func(*server.Server)) *env {
	t.Helper()
	srv := server.New(db, s, testVersion)
	for _, f := range tune {
		f(srv)
	}
	ln, err := server.Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop when its context was cancelled")
		}
	})
	return &env{t: t, db: db, srv: srv, ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
}

// started is the usual: the main corpus with the default settings.
func started(t *testing.T, tune ...func(*server.Server)) *env {
	t.Helper()
	db, root := mainCorpus(t)
	e := start(t, db, defaults(), tune...)
	e.root = root
	return e
}

// resp is one answer, read whole.
type resp struct {
	status int
	header http.Header
	body   string
}

// get asks for a path; hdr sets headers and "Host" there sets the Host header.
func (e *env) get(path string, hdr ...string) resp { return e.do("GET", path, hdr...) }

func (e *env) do(method, path string, hdr ...string) resp {
	e.t.Helper()
	req, err := http.NewRequest(method, e.base()+path, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if strings.EqualFold(hdr[i], "Host") {
			req.Host = hdr[i+1]
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 20 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return resp{r.StatusCode, r.Header, string(b)}
}

// raw sends text as the whole request and reads what comes back (status line and headers, then the body).
func (e *env) raw(text string) resp {
	e.t.Helper()
	c, err := net.DialTimeout("tcp", e.addr(), 5*time.Second)
	if err != nil {
		e.t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	io.WriteString(c, text)
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return resp{status: 0}
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return resp{r.StatusCode, r.Header, string(b)}
}

// jsonOf decodes a JSON body into a generic object.
func jsonOf(t *testing.T, r resp) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, r.body)
	}
	return m
}

func must(t *testing.T, r resp, status int) resp {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d\n%s", r.status, status, r.body)
	}
	return r
}

var mdLink = regexp.MustCompile(`\]\(([^)\s]*)\)`)
var htmlHref = regexp.MustCompile(`href="([^"]*)"`)

// linksOf are the link targets of a Markdown or HTML body. An HTML page is read
// for its href attributes only, a Markdown page for ](target) with the escaped
// brackets (\[ and \]) taken out first, since those are text and not links.
func linksOf(body string) []string {
	var out []string
	if strings.HasPrefix(body, "<!doctype") {
		for _, m := range htmlHref.FindAllStringSubmatch(body, -1) {
			out = append(out, html.UnescapeString(m[1]))
		}
		return out
	}
	body = strings.NewReplacer(`\[`, "", `\]`, "").Replace(body)
	for _, m := range mdLink.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// resolve resolves a link against the address of the page it is on.
func resolve(t *testing.T, e *env, page, link string) string {
	t.Helper()
	b, err := url.Parse(e.base() + page)
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return r.String()
}

func ctxT(t *testing.T) context.Context { return context.Background() }
func tocLimits() toc.Limits             { return toc.Limits{} }

func queryEscape(s string) string { return url.QueryEscape(s) }
