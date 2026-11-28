package server_test

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

// ---- who may ask ----------------------------------------------------------------

func TestTheListenerIsLoopbackOnlyAndRefusesTheMachinesOwnAddress(t *testing.T) {
	e := started(t)
	if ip := e.ln.Addr().(*net.TCPAddr).IP; !ip.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("listening on %v, want 127.0.0.1", ip)
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ipn.IP, e.port), 2e9)
		if err == nil {
			c.Close()
			t.Errorf("the server answers on %v, which is not loopback", ipn.IP)
		}
		return
	}
	t.Log("no non-loopback address on this machine to try")
}

func TestTheHostHeaderMustBeLocalhostOrLoopbackWithTheBoundPort(t *testing.T) {
	e := started(t)
	p := fmt.Sprint(e.port)
	for host, want := range map[string]int{
		"127.0.0.1:" + p:          200,
		"localhost:" + p:          200,
		"LOCALHOST:" + p:          200,
		"127.0.0.1:1":             403,
		"localhost:1":             403,
		"127.0.0.1":               403,
		"localhost":               403,
		"evil.example":            403,
		"evil.example:" + p:       403,
		"localhost.:" + p:         403,
		"127.0.0.1." + p:          403,
		"127.0.0.1:" + p + ".x":   403,
		"[::1]:" + p:              403,
		"0.0.0.0:" + p:            403,
		"127.0.0.1:" + p + ",a:b": 403,
	} {
		if got := e.get("/search?q=tomatoes", "Host", host).status; got != want {
			t.Errorf("Host %q: %d, want %d", host, got, want)
		}
	}
	if got := e.raw("GET /search?q=tomatoes HTTP/1.0\r\n\r\n").status; got != 403 {
		t.Errorf("no Host header: %d, want 403", got)
	}
	// every path and format is guarded alike
	for _, path := range []string{"/", "/search", "/search.json?q=a", "/search.html?q=a", "/opensearch.xml", "/version", "/stats", "/sets", "/nothing"} {
		if got := e.get(path, "Host", "evil.example").status; got != 403 {
			t.Errorf("%s with a foreign Host: %d, want 403", path, got)
		}
	}
}

func TestACrossSiteRequestIsRefusedAndTheOthersPass(t *testing.T) {
	e := started(t)
	for _, tc := range []struct {
		site string
		want int
	}{{"cross-site", 403}, {"same-site", 403}, {"none", 200}, {"same-origin", 200}, {"", 200}} {
		var got int
		if tc.site == "" {
			got = e.get("/search?q=tomatoes").status
		} else {
			got = e.get("/search?q=tomatoes", "Sec-Fetch-Site", tc.site).status
		}
		if got != tc.want {
			t.Errorf("Sec-Fetch-Site %q: %d, want %d", tc.site, got, tc.want)
		}
	}
}

// ---- headers, methods and addresses -------------------------------------------------

func TestNoResponseCarriesACORSHeaderAndEveryOneCarriesTheSafetyHeaders(t *testing.T) {
	e := started(t)
	for _, tc := range []struct {
		method, path string
		hdr          []string
	}{
		{"GET", "/search?q=tomatoes", nil}, {"GET", "/search.html?q=tomatoes", nil}, {"GET", "/search.json?q=tomatoes", nil},
		{"GET", "/", nil}, {"GET", "/opensearch.xml", nil}, {"GET", "/version", nil}, {"GET", "/stats", nil}, {"GET", "/sets", nil},
		{"GET", "/search?q=" + strings.Repeat("a", 1025), nil}, // 400
		{"GET", "/nothing", nil},                               // 404
		{"POST", "/search", nil},                               // 405
		{"OPTIONS", "/search", []string{"Origin", "https://evil.example", "Access-Control-Request-Method", "GET"}},
		{"GET", "/search", []string{"Accept", "image/png"}},      // 406
		{"GET", "/search?q=x", []string{"Host", "evil.example"}}, // 403
		{"GET", "/search?q=x", []string{"Origin", "https://evil.example"}},
	} {
		r := e.do(tc.method, tc.path, tc.hdr...)
		for k := range r.header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("%s %s (%d) sent %s", tc.method, tc.path, r.status, k)
			}
		}
		for k, v := range map[string]string{
			"Cache-Control":                "no-store",
			"X-Content-Type-Options":       "nosniff",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Resource-Policy": "same-origin",
		} {
			if got := r.header.Get(k); got != v {
				t.Errorf("%s %s (%d): %s is %q, want %q", tc.method, tc.path, r.status, k, got, v)
			}
		}
	}
}

func TestHTMLCarriesAContentSecurityPolicyThatAllowsNothingButItsOwnForm(t *testing.T) {
	e := started(t)
	csp := must(t, e.get("/search.html?q=tomatoes"), 200).header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "form-action 'self'", "base-uri 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("policy %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "style-src") || strings.Contains(csp, "script-src") || strings.Contains(csp, "unsafe") {
		t.Errorf("policy %q allows more than it should with no stylesheet set", csp)
	}
}

func TestOnlyGetAndHeadAreAnsweredAndHeadHasNoBody(t *testing.T) {
	e := started(t)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE"} {
		r := e.do(m, "/search?q=tomatoes")
		if r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: %d, Allow %q, want 405 and GET, HEAD", m, r.status, r.header.Get("Allow"))
		}
	}
	g := must(t, e.get("/search?q=tomatoes"), 200)
	h := must(t, e.do("HEAD", "/search?q=tomatoes"), 200)
	if h.body != "" || h.header.Get("Content-Type") != g.header.Get("Content-Type") {
		t.Errorf("HEAD: body %d bytes, type %q, want none and %q", len(h.body), h.header.Get("Content-Type"), g.header.Get("Content-Type"))
	}
	for _, p := range []string{"/nothing", "/search/extra", "/../etc/passwd", "/opensearch.json", "/index.json", "/search.xml", "//search"} {
		if got := e.get(p).status; got != 404 && got != 406 && got != 301 {
			t.Errorf("%s: %d, want 404", p, got)
		}
	}
}

// ---- formats -----------------------------------------------------------------------

func TestTheFormatComesFromTheSuffixThenTheAcceptHeaderAndMarkdownIsTheDefault(t *testing.T) {
	e := started(t)
	type tc struct {
		path, accept, want string
		status             int
	}
	for _, c := range []tc{
		{"/search?q=tomatoes", "", "text/markdown; charset=utf-8", 200},
		{"/search?q=tomatoes", "*/*", "text/markdown; charset=utf-8", 200},
		{"/search?q=tomatoes", "text/markdown", "text/markdown; charset=utf-8", 200},
		{"/search?q=tomatoes", "text/html", "text/html; charset=utf-8", 200},
		{"/search?q=tomatoes", "application/json", "application/json", 200},
		{"/search?q=tomatoes", "text/html;q=0.5, application/json;q=0.9", "application/json", 200},
		{"/search?q=tomatoes", "text/html, application/json;q=0.1", "text/html; charset=utf-8", 200},
		{"/search?q=tomatoes", "image/png, text/html", "text/html; charset=utf-8", 200},
		{"/search?q=tomatoes", "image/png", "", 406},
		{"/search?q=tomatoes", "text/plain", "", 406},
		// the suffix picks the format unless the client's Accept rules it out; then the
		// best format the client does accept is served (BonBon asks for text/markdown only)
		{"/search.md?q=tomatoes", "application/json", "application/json", 200},
		{"/search.html?q=tomatoes", "application/json", "application/json", 200},
		{"/search.json?q=tomatoes", "text/html", "text/html; charset=utf-8", 200},
		{"/search.json?q=tomatoes", "image/png", "", 406},
		{"/search.html?q=tomatoes", "text/markdown, text/x-opml", "text/markdown; charset=utf-8", 200},
		{"/search.json?q=tomatoes", "text/markdown, text/x-opml", "text/markdown; charset=utf-8", 200},
		{"/index.html", "text/markdown, text/x-opml", "text/markdown; charset=utf-8", 200},
		{"/version.html", "text/markdown, text/x-opml", "text/markdown; charset=utf-8", 200},
		{"/search.html?q=tomatoes", "text/markdown, text/x-opml, */*", "text/html; charset=utf-8", 200},
		{"/search.html?q=tomatoes", "text/*", "text/html; charset=utf-8", 200},
		{"/search.html?q=tomatoes", "text/markdown, text/html;q=0", "text/markdown; charset=utf-8", 200},
		{"/search.json?q=tomatoes", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", "application/json", 200},
		{"/search.md?q=tomatoes", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", "text/markdown; charset=utf-8", 200},
		{"/search.html?q=tomatoes", "", "text/html; charset=utf-8", 200},
		{"/search.html?q=tomatoes", "garbage", "text/html; charset=utf-8", 200},
	} {
		r := e.get(c.path, "Accept", c.accept)
		if r.status != c.status {
			t.Errorf("%s with Accept %q: %d, want %d", c.path, c.accept, r.status, c.status)
			continue
		}
		if c.want != "" && !strings.HasPrefix(r.header.Get("Content-Type"), strings.TrimSuffix(c.want, "; charset=utf-8")) {
			t.Errorf("%s with Accept %q: type %q, want %q", c.path, c.accept, r.header.Get("Content-Type"), c.want)
		}
		if c.status == 200 && !strings.Contains(r.header.Get("Vary"), "Accept") {
			t.Errorf("%s: Vary is %q, want it to name Accept", c.path, r.header.Get("Vary"))
		}
	}
}

func TestTheRootIsTheSearchPageAndSoIsASearchWithNoQuery(t *testing.T) {
	e := started(t)
	root := must(t, e.get("/"), 200)
	bare := must(t, e.get("/search"), 200)
	if root.body != bare.body {
		t.Errorf("/ and /search differ:\n%s\n---\n%s", root.body, bare.body)
	}
	for _, want := range []string{"/search", "/opensearch.xml"} {
		if !strings.Contains(root.body, want) {
			t.Errorf("the search page does not mention %s:\n%s", want, root.body)
		}
	}
	for _, leak := range []string{"a.example", e.root, "250", "tomatoes"} {
		if strings.Contains(root.body, leak) {
			t.Errorf("the search page names %q, which is the corpus", leak)
		}
	}
	html := must(t, e.get("/index.html"), 200)
	if !strings.HasPrefix(html.header.Get("Content-Type"), "text/html") || !strings.Contains(html.body, `<form`) || !strings.Contains(html.body, `action="/search.html"`) {
		t.Errorf("/index.html:\n%s", html.body)
	}
	if got := e.get("/", "Accept", "application/json").status; got != 406 {
		t.Errorf("/ as JSON: %d, want 406", got)
	}
	if got := e.get("/index.json").status; got != 404 && got != 406 {
		t.Errorf("/index.json: %d, want 404 or 406", got)
	}
}

// ---- discovery: the Link header ------------------------------------------------------

const linkValue = `</opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="cmarkfind"`

func TestEveryResponseNamesTheDescriptionInALinkHeader(t *testing.T) {
	e := started(t)
	for _, tc := range []struct {
		method, path string
		hdr          []string
	}{
		{"GET", "/search?q=tomatoes", nil}, {"GET", "/search.html?q=tomatoes", nil}, {"GET", "/search.json?q=tomatoes", nil},
		{"GET", "/", nil}, {"GET", "/index.html", nil}, {"GET", "/version", nil}, {"GET", "/stats.json", nil}, {"GET", "/sets.html", nil},
		{"GET", "/opensearch.xml", nil}, {"HEAD", "/search?q=tomatoes", nil}, {"HEAD", "/", nil},
		{"GET", "/search?q=tomatoes", []string{"Accept", "text/markdown, text/x-opml"}}, // BonBon's own
		{"GET", "/search?q=" + strings.Repeat("a", 1025), nil},                          // 400
		{"GET", "/nothing", nil},                                 // 404
		{"POST", "/search", nil},                                 // 405
		{"GET", "/search", []string{"Accept", "image/png"}},      // 406
		{"GET", "/search?q=x", []string{"Host", "evil.example"}}, // 403
	} {
		r := e.do(tc.method, tc.path, tc.hdr...)
		if got := r.header.Values("Link"); len(got) != 1 || got[0] != linkValue {
			t.Errorf("%s %s (%d): Link is %q, want exactly %q", tc.method, tc.path, r.status, got, linkValue)
		}
	}
}

// A client reads the header from any answer, resolves its target against the
// address it asked, and finds the description there.
func TestTheLinkHeaderLeadsAMarkdownClientToTheDescriptionItCanUse(t *testing.T) {
	e := started(t)
	bonbon := []string{"Accept", "text/markdown, text/x-opml"}
	page := must(t, e.get("/search?q=tomatoes", bonbon...), 200)
	if !strings.HasPrefix(page.header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("a Markdown client got %q", page.header.Get("Content-Type"))
	}
	link := page.header.Get("Link")
	target := link[strings.Index(link, "<")+1 : strings.Index(link, ">")]
	for _, part := range []string{`rel="search"`, `type="application/opensearchdescription+xml"`} {
		if !strings.Contains(link, part) {
			t.Errorf("Link %q lacks %s", link, part)
		}
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() {
		t.Fatalf("the target %q should be a relative reference", target)
	}
	desc := must(t, e.get(strings.TrimPrefix(resolve(t, e, "/search", target), e.base()), bonbon...), 200)
	if !strings.HasPrefix(desc.header.Get("Content-Type"), "application/opensearchdescription+xml") {
		t.Errorf("the linked description is %q", desc.header.Get("Content-Type"))
	}
	var d osd
	if err := xml.Unmarshal([]byte(desc.body), &d); err != nil || len(d.Urls) == 0 || d.Urls[0].Type != "text/markdown" {
		t.Errorf("the linked description is not the one a Markdown client can use: %v\n%.300s", err, desc.body)
	}
	// from the template the client can search, and gets Markdown
	tmpl := strings.NewReplacer("{searchTerms}", "tomatoes", "{count?}", "", "{startIndex?}", "").Replace(d.Urls[0].Template)
	found := must(t, e.get(strings.TrimPrefix(tmpl, e.base()), bonbon...), 200)
	if !strings.HasPrefix(found.header.Get("Content-Type"), "text/markdown") || !strings.Contains(found.body, "Hits 1 to 100 of 250") {
		t.Errorf("searching by the template: %q\n%.200s", found.header.Get("Content-Type"), found.body)
	}
}
