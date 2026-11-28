package server

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/internal/render"
)

// paging is where a page of results is among all of them (item 5).
type paging struct {
	Query      string
	Start      int
	Count      int
	Total      int
	Shown      int
	Prev, Next string // relative references, or empty
}

// pageOf works out the position line's numbers and the neighbours' references.
func pageOf(query, set string, start, count int, res *corpus.Results) paging {
	p := paging{Query: query, Start: start, Count: count, Total: res.Total, Shown: len(res.Hits)}
	ref := func(at int) string {
		r := "?q=" + url.QueryEscape(query) + "&startIndex=" + strconv.Itoa(at) + "&count=" + strconv.Itoa(count)
		if set != "" {
			r += "&set=" + url.QueryEscape(set)
		}
		return r
	}
	if start > 1 {
		p.Prev = ref(max(1, start-count))
	}
	if start+count-1 < res.Total {
		p.Next = ref(start + count)
	}
	return p
}

// withCommas writes a number with a comma between thousands.
func withCommas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// position is the line that says which hits the page shows.
func (p paging) position() string {
	switch {
	case p.Total == 0:
		return "No matches"
	case p.Shown == 0:
		return "No hits on this page; there are " + withCommas(p.Total)
	}
	return fmt.Sprintf("Hits %s to %s of %s", withCommas(p.Start), withCommas(p.Start+p.Shown-1), withCommas(p.Total))
}

// markdownResults is the Markdown of a page of results: the position line, what
// `cmarkfind search` prints, and the links to the neighbouring pages.
func markdownResults(p paging, res *corpus.Results, set string) string {
	var b strings.Builder
	b.WriteString(p.position() + "\n\n")
	if len(res.Hits) > 0 {
		render.Body(&b, res, set)
		b.WriteString("\n")
	}
	if p.Prev != "" {
		fmt.Fprintf(&b, "[Previous](%s)\n\n", p.Prev)
	}
	if p.Next != "" {
		fmt.Fprintf(&b, "[Next](%s)\n\n", p.Next)
	}
	return b.String()
}

// root is the small search page of / and of a search with no query (item 2): it
// says how to search and names nothing of the corpus.
func (s *Server) root(w http.ResponseWriter, r *http.Request, f format) {
	if f == formatHTML {
		s.writeHTML(w, htmlRoot(s.set.CSS, s.set.Info))
		return
	}
	var b strings.Builder
	b.WriteString("# cmarkfind\n\nSearch the Markdown pages in this cmarkfind corpus.\n\n")
	b.WriteString("- Search: `/search?q=QUERY`, for example [/search?q=example](/search?q=example). The page, `.html` and `.json` are the Markdown, HTML and JSON of one search; `set`, `count` and `startIndex` are the other parameters.\n")
	b.WriteString("- The description for a browser's search box: [/opensearch.xml](/opensearch.xml)\n")
	if s.set.Info {
		b.WriteString("- The program's version: [/version](/version)\n- This server's settings and counts: [/stats](/stats)\n- The sets that can be searched: [/sets](/sets)\n")
	}
	reply(w, f, b.String())
}

// ---- HTML ---------------------------------------------------------------------------------

type htmlHit struct {
	Title, Address, Href, Detail string
	Snippet                      template.HTML
}

type htmlFacetValue struct {
	Value, Href string
	Count       int
}

type htmlFacet struct {
	Label  string
	Values []htmlFacetValue
}

type htmlPage struct {
	Title, CSS, Q, Set, Position, Prev, Next string
	Info                                     bool
	Root                                     bool
	Start                                    int
	Facets                                   []htmlFacet
	Hits                                     []htmlHit
	Rows                                     [][]string // a table, for /stats and /sets
	Head                                     []string
	Lines                                    []string // plain lines, for /version
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="search" type="application/opensearchdescription+xml" title="cmarkfind" href="/opensearch.xml">
{{if .CSS}}<link rel="stylesheet" href="{{.CSS}}">
{{end}}</head>
<body>
<h1>cmarkfind</h1>
<form action="/search.html" method="get">
<input type="search" name="q" value="{{.Q}}" aria-label="Search">
{{if .Set}}<input type="hidden" name="set" value="{{.Set}}">
{{end}}<button type="submit">Search</button>
</form>
{{if .Root}}<p>Search the Markdown pages in this cmarkfind corpus. The same search is at <a href="/search?q=example">/search</a> as Markdown, <a href="/search.html?q=example">/search.html</a> and <a href="/search.json?q=example">/search.json</a>.</p>
<p>The description for a browser's search box is at <a href="/opensearch.xml">/opensearch.xml</a>.</p>
{{if .Info}}<ul>
<li><a href="/version.html">version</a></li>
<li><a href="/stats.html">stats</a></li>
<li><a href="/sets.html">sets</a></li>
</ul>
{{end}}{{end}}{{if .Position}}<p>{{.Position}}</p>
{{end}}{{if .Lines}}<dl>
{{range .Lines}}<dd>{{.}}</dd>
{{end}}</dl>
{{end}}{{if .Rows}}<table>
<tr>{{range .Head}}<th>{{.}}</th>{{end}}</tr>
{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
{{end}}</table>
{{end}}{{if .Facets}}<h2>Facets</h2>
<ul>
{{range .Facets}}<li>{{.Label}}<ul>
{{range .Values}}<li><a href="{{.Href}}">{{.Value}} ({{.Count}})</a></li>
{{end}}</ul></li>
{{end}}</ul>
{{end}}{{if .Hits}}<h2>Results</h2>
<ol start="{{.Start}}">
{{range .Hits}}<li>{{if .Href}}<a href="{{.Href}}" rel="noopener noreferrer">{{.Title}}</a>{{else}}{{.Title}} <code>{{.Address}}</code>{{end}} — {{.Detail}}{{if .Snippet}}<br>
{{.Snippet}}{{end}}</li>
{{end}}</ol>
{{end}}{{if or .Prev .Next}}<p>{{if .Prev}}<a href="{{.Prev}}">Previous</a>{{end}}{{if and .Prev .Next}} {{end}}{{if .Next}}<a href="{{.Next}}">Next</a>{{end}}</p>
{{end}}</body>
</html>
`))

// writeHTML renders a page; a content security policy goes with it (item 9).
func (s *Server) writeHTML(w http.ResponseWriter, p htmlPage) {
	var b bytes.Buffer
	if err := pageTemplate.Execute(&b, p); err != nil {
		s.logf("internal error: %v", err)
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	csp := "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	if o := cssOrigin(p.CSS); o != "" {
		csp += "; style-src " + o
	}
	w.Header().Set("Content-Security-Policy", csp)
	reply(w, formatHTML, b.String())
}

func htmlRoot(css string, info bool) htmlPage {
	return htmlPage{Title: "cmarkfind", CSS: css, Root: true, Info: info}
}

// webAddress is true for an address a browser can open.
func webAddress(a string) bool {
	return strings.HasPrefix(a, "https://") || strings.HasPrefix(a, "http://")
}

// markedSnippet turns a snippet's [matched] words into <mark> and escapes all
// the rest: stored text never becomes markup of its own.
func markedSnippet(s string) template.HTML {
	s = render.Clean(strings.Join(strings.Fields(s), " "))
	var b strings.Builder
	open := false
	for _, r := range s {
		switch {
		case r == '[' && !open:
			b.WriteString("<mark>")
			open = true
		case r == ']' && open:
			b.WriteString("</mark>")
			open = false
		default:
			b.WriteString(html.EscapeString(string(r)))
		}
	}
	if open {
		b.WriteString("</mark>")
	}
	return template.HTML(b.String())
}

func htmlResults(css string, p paging, res *corpus.Results, set string) htmlPage {
	pg := htmlPage{Title: "Search: " + render.Clean(p.Query), CSS: css, Q: render.Clean(p.Query), Set: set, Position: p.position(), Prev: p.Prev, Next: p.Next, Start: p.Start}
	for _, f := range res.Facets {
		hf := htmlFacet{Label: render.Clean(f.Label)}
		for _, v := range f.Values {
			href := "?q=" + url.QueryEscape(v.Query)
			if set != "" {
				href += "&set=" + url.QueryEscape(set)
			}
			hf.Values = append(hf.Values, htmlFacetValue{Value: render.Clean(v.Value), Href: href, Count: v.Count})
		}
		pg.Facets = append(pg.Facets, hf)
	}
	for _, h := range res.Hits {
		hh := htmlHit{Title: strings.TrimSpace(render.Clean(h.Title)), Address: render.Clean(h.Address), Snippet: markedSnippet(h.Snippet)}
		if hh.Title == "" {
			hh.Title = hh.Address
		}
		if webAddress(h.Address) {
			hh.Href = h.Address
			if h.Anchor != "" {
				hh.Href += "#" + h.Anchor
			}
		}
		hh.Detail = h.Tier
		if h.Pinned {
			hh.Detail += ", pinned"
		}
		if len(h.Tags) > 0 {
			hh.Detail += ", tags: " + render.Clean(strings.Join(h.Tags, " "))
		}
		pg.Hits = append(pg.Hits, hh)
	}
	return pg
}
