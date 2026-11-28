package server_test

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// ---- parameters -----------------------------------------------------------------

func TestEachBadParameterIsA400ThatNamesIt(t *testing.T) {
	e := started(t)
	for _, tc := range []struct{ query, names string }{
		{"q=" + strings.Repeat("a", 1025), "q"},
		{"q=tomatoes&count=0", "count"},
		{"q=tomatoes&count=-1", "count"},
		{"q=tomatoes&count=abc", "count"},
		{"q=tomatoes&count=1.5", "count"},
		{"q=tomatoes&count=101", "count"}, // over the results setting, 100
		{"q=tomatoes&startIndex=0", "startIndex"},
		{"q=tomatoes&startIndex=-3", "startIndex"},
		{"q=tomatoes&startIndex=abc", "startIndex"},
		{"q=tomatoes&set=no-such-set", "set"},
	} {
		for _, suffix := range []string{"", ".json", ".html"} {
			r := e.get("/search" + suffix + "?" + tc.query)
			if r.status != 400 || !strings.Contains(r.body, tc.names) {
				t.Errorf("/search%s?%.60s: %d %q, want 400 naming %s", suffix, tc.query, r.status, r.body, tc.names)
			}
		}
	}
}

func TestParametersThatAreOnlyOddAreNotErrors(t *testing.T) {
	e := started(t)
	for name, q := range map[string]string{
		"an unknown parameter":      "q=tomatoes&nope=1",
		"an empty count":            "q=tomatoes&count=",
		"an empty startIndex":       "q=tomatoes&startIndex=",
		"an empty set":              "q=tomatoes&set=",
		"a query of exactly 1,024":  "q=" + strings.Repeat("a", 1024),
		"a bad query read as words": "q=" + url.QueryEscape(`tomatoes AND ( "unclosed`),
		"a no-match":                "q=zebra",
	} {
		if r := e.get("/search.json?" + q); r.status != 200 {
			t.Errorf("%s: %d\n%s", name, r.status, r.body)
		}
	}
	// a repeated parameter uses its first value
	r := must(t, e.get("/search.json?q=tomatoes&q=zebra&count=3&count=50"), 200)
	j := jsonOf(t, r)
	if j["totalResults"] != float64(250) || len(j["hits"].([]any)) != 3 {
		t.Errorf("a repeated q or count: total %v, %d hits", j["totalResults"], len(j["hits"].([]any)))
	}
	// no matches says so, in every format
	if r := must(t, e.get("/search?q=zebra"), 200); !strings.Contains(r.body, "No matches") {
		t.Errorf("Markdown:\n%s", r.body)
	}
	if r := must(t, e.get("/search.html?q=zebra"), 200); !strings.Contains(r.body, "No matches") {
		t.Errorf("HTML:\n%s", r.body)
	}
	if j := jsonOf(t, must(t, e.get("/search.json?q=zebra"), 200)); j["totalResults"] != float64(0) || len(j["hits"].([]any)) != 0 {
		t.Errorf("JSON: %v", j)
	}
}

func TestSetSearchesOneSetAndFacetLinksKeepIt(t *testing.T) {
	db, root := mainCorpus(t)
	other := writeSite(t, "b.example", map[string]string{"x.md": "# X\n\ntomatoes elsewhere\n", "y.md": "# Y\n\ntomatoes again\n"})
	if _, err := db.FillLocal(ctxT(t), other, tocLimits()); err != nil {
		t.Fatal(err)
	}
	e := start(t, db, defaults())
	all := jsonOf(t, must(t, e.get("/search.json?q=tomatoes&count=1"), 200))
	if all["totalResults"] != float64(252) {
		t.Fatalf("every set: total %v, want 252", all["totalResults"])
	}
	one := jsonOf(t, must(t, e.get("/search.json?q=tomatoes&set="+url.QueryEscape(other)), 200))
	if one["totalResults"] != float64(2) {
		t.Errorf("the other set: total %v, want 2", one["totalResults"])
	}
	main := must(t, e.get("/search?q=tomatoes&set="+url.QueryEscape(root)), 200)
	var facetLinks int
	for _, l := range linksOf(main.body) {
		if strings.HasPrefix(l, "?q=") {
			facetLinks++
			if !strings.Contains(l, "set="+url.QueryEscape(root)) {
				t.Errorf("a facet link of a one-set search loses the set: %s", l)
			}
		}
	}
	if facetLinks == 0 {
		t.Errorf("no facet links on the page:\n%s", main.body[:min(len(main.body), 800)])
	}
}

// ---- paging -------------------------------------------------------------------------

func TestPagingReportsTheTotalAndWalksTheListInEveryFormat(t *testing.T) {
	e := started(t) // 250 matches, results 100
	type page struct{ start, items, prevStart, nextStart int }
	var walked []string
	for _, p := range []page{{1, 100, 0, 101}, {101, 100, 1, 201}, {201, 50, 101, 0}} {
		j := jsonOf(t, must(t, e.get(fmt.Sprintf("/search.json?q=tomatoes&startIndex=%d", p.start)), 200))
		if j["totalResults"] != float64(250) || j["startIndex"] != float64(p.start) || j["itemsPerPage"] != float64(100) {
			t.Errorf("start %d: total %v, startIndex %v, itemsPerPage %v", p.start, j["totalResults"], j["startIndex"], j["itemsPerPage"])
		}
		hits := j["hits"].([]any)
		if len(hits) != p.items {
			t.Errorf("start %d: %d hits, want %d", p.start, len(hits), p.items)
		}
		for _, h := range hits {
			walked = append(walked, h.(map[string]any)["address"].(string))
		}
		for key, want := range map[string]int{"previous": p.prevStart, "next": p.nextStart} {
			ref, _ := j[key].(string)
			if want == 0 {
				if j[key] != nil {
					t.Errorf("start %d: %s is %v, want null", p.start, key, j[key])
				}
				continue
			}
			u, err := url.Parse(ref)
			if err != nil || !strings.HasPrefix(ref, "?") || u.Query().Get("q") != "tomatoes" || u.Query().Get("startIndex") != fmt.Sprint(want) {
				t.Errorf("start %d: %s is %q, want a relative ?q=tomatoes&startIndex=%d", p.start, key, ref, want)
			}
		}
	}
	seen := map[string]bool{}
	for _, a := range walked {
		if seen[a] {
			t.Fatalf("%s is on two pages", a)
		}
		seen[a] = true
	}
	if len(walked) != 250 {
		t.Errorf("the three pages hold %d hits, want 250", len(walked))
	}
	// past the end: not an error, and the total is kept
	past := jsonOf(t, must(t, e.get("/search.json?q=tomatoes&startIndex=301"), 200))
	if past["totalResults"] != float64(250) || len(past["hits"].([]any)) != 0 || past["next"] != nil {
		t.Errorf("past the end: %v", past)
	}

	// Markdown and HTML say where they are and link the neighbours
	for _, suffix := range []string{"", ".html"} {
		page2 := must(t, e.get("/search"+suffix+"?q=tomatoes&startIndex=101"), 200).body
		if !strings.Contains(page2, "Hits 101 to 200 of 250") {
			t.Errorf("/search%s page 2 lacks its position line:\n%.600s", suffix, page2)
		}
		var prev, next bool
		for _, l := range linksOf(page2) {
			if strings.HasPrefix(l, "?") && strings.Contains(l, "startIndex=1") && !strings.Contains(l, "startIndex=101") && !strings.Contains(l, "startIndex=201") {
				prev = true
			}
			if strings.HasPrefix(l, "?") && strings.Contains(l, "startIndex=201") {
				next = true
			}
		}
		if !prev || !next {
			t.Errorf("/search%s page 2: previous %v, next %v", suffix, prev, next)
		}
		last := must(t, e.get("/search"+suffix+"?q=tomatoes&startIndex=201"), 200).body
		if !strings.Contains(last, "Hits 201 to 250 of 250") {
			t.Errorf("/search%s last page:\n%.400s", suffix, last)
		}
		for _, l := range linksOf(last) {
			if strings.HasPrefix(l, "?") && strings.Contains(l, "startIndex=251") {
				t.Errorf("/search%s: a Next link on the last page: %s", suffix, l)
			}
		}
		if beyond := must(t, e.get("/search"+suffix+"?q=tomatoes&startIndex=1001"), 200).body; !strings.Contains(beyond, "No hits on this page; there are 250") {
			t.Errorf("/search%s past the end:\n%.400s", suffix, beyond)
		}
	}
}

func TestCountSetsThePageSizeWithinTheResultsSettingAndNeverChangesTheTotal(t *testing.T) {
	e := started(t)
	j := jsonOf(t, must(t, e.get("/search.json?q=tomatoes&count=30&startIndex=31"), 200))
	if j["totalResults"] != float64(250) || j["itemsPerPage"] != float64(30) || len(j["hits"].([]any)) != 30 {
		t.Errorf("count 30: %v %v %d", j["totalResults"], j["itemsPerPage"], len(j["hits"].([]any)))
	}
	prev, _ := url.Parse(j["previous"].(string))
	next, _ := url.Parse(j["next"].(string))
	if prev.Query().Get("startIndex") != "1" || prev.Query().Get("count") != "30" || next.Query().Get("startIndex") != "61" || next.Query().Get("count") != "30" {
		t.Errorf("the links lose count: previous %v, next %v", j["previous"], j["next"])
	}
	// the results setting is the cap and the default
	db, _ := mainCorpus(t)
	s := defaults()
	s.Results = 40
	e2 := start(t, db, s)
	j = jsonOf(t, must(t, e2.get("/search.json?q=tomatoes"), 200))
	if j["itemsPerPage"] != float64(40) || len(j["hits"].([]any)) != 40 || j["totalResults"] != float64(250) {
		t.Errorf("results 40: %v %d %v", j["itemsPerPage"], len(j["hits"].([]any)), j["totalResults"])
	}
	if r := e2.get("/search.json?q=tomatoes&count=41"); r.status != 400 {
		t.Errorf("count over the setting: %d", r.status)
	}
	if r := e2.get("/search.json?q=tomatoes&count=40"); r.status != 200 {
		t.Errorf("count at the setting: %d", r.status)
	}
}

// ---- facets -----------------------------------------------------------------------------

func TestAFacetLinkResolvedAgainstItsPageIsTheSameEndpointAndFormatAndFindsTheCountedPages(t *testing.T) {
	e := started(t)
	for _, tc := range []struct{ path, kind string }{{"/search?q=tomatoes", "text/markdown"}, {"/search.html?q=tomatoes", "text/html"}, {"/search.md?q=tomatoes&startIndex=101", "text/markdown"}} {
		page := must(t, e.get(tc.path), 200)
		var found int
		for _, l := range linksOf(page.body) {
			if !strings.HasPrefix(l, "?q=") {
				continue
			}
			if lu, _ := url.Parse(l); lu.Query().Get("q") == "tomatoes" {
				continue // Previous and Next: the same query, another page; a facet adds a clause
			}
			target := resolve(t, e, tc.path[:strings.Index(tc.path, "?")], l)
			u, _ := url.Parse(target)
			if u.Path != tc.path[:strings.Index(tc.path, "?")] {
				t.Errorf("%s: the facet link %s leaves the endpoint: %s", tc.path, l, u.Path)
			}
			if u.Query().Get("startIndex") != "" {
				t.Errorf("%s: a facet link keeps startIndex: %s", tc.path, l)
			}
			r := must(t, e.get(strings.TrimPrefix(target, e.base())), 200)
			if !strings.HasPrefix(r.header.Get("Content-Type"), tc.kind) {
				t.Errorf("%s: the facet page is %s, want %s", tc.path, r.header.Get("Content-Type"), tc.kind)
			}
			found++
		}
		if found < 2 {
			t.Errorf("%s: %d facet links, want the two values of Topics", tc.path, found)
		}
	}
	// the counted pages: go is on the 125 even pages
	j := jsonOf(t, must(t, e.get("/search.json?q=tomatoes"), 200))
	facets := j["facets"].(map[string]any)
	kw := facets["keywords"].(map[string]any)
	if kw["label"] != "Topics" {
		t.Errorf("label %v", kw["label"])
	}
	for _, v := range kw["values"].([]any) {
		val := v.(map[string]any)
		again := jsonOf(t, must(t, e.get("/search.json?q="+url.QueryEscape(val["query"].(string))), 200))
		if again["totalResults"] != val["count"] {
			t.Errorf("facet %v counts %v, its query finds %v", val["value"], val["count"], again["totalResults"])
		}
	}
}

// ---- the OpenSearch description ----------------------------------------------------------

type osd struct {
	XMLName     xml.Name `xml:"OpenSearchDescription"`
	ShortName   string
	Description string
	Input       string `xml:"InputEncoding"`
	Output      string `xml:"OutputEncoding"`
	Urls        []struct {
		Type     string `xml:"type,attr"`
		Template string `xml:"template,attr"`
		Rel      string `xml:"rel,attr"`
	} `xml:"Url"`
}

func TestTheDescriptionDocumentIsOpenSearchWithFourUrlsBuiltForTheHostAsked(t *testing.T) {
	e := started(t)
	for _, host := range []string{e.addr(), fmt.Sprintf("localhost:%d", e.port)} {
		r := must(t, e.get("/opensearch.xml", "Host", host), 200)
		if r.header.Get("Content-Type") != "application/opensearchdescription+xml; charset=utf-8" {
			t.Errorf("type %q", r.header.Get("Content-Type"))
		}
		var d osd
		if err := xml.Unmarshal([]byte(r.body), &d); err != nil {
			t.Fatalf("not XML: %v\n%s", err, r.body)
		}
		if d.XMLName.Space != "http://a9.com/-/spec/opensearch/1.1/" {
			t.Errorf("namespace %q", d.XMLName.Space)
		}
		if d.ShortName != "cmarkfind" || len(d.ShortName) > 16 || d.Description == "" || len(d.Description) > 1024 || d.Input != "UTF-8" || d.Output != "UTF-8" {
			t.Errorf("document: %+v", d)
		}
		for _, leak := range []string{"a.example", e.root, "250"} {
			if strings.Contains(r.body, leak) {
				t.Errorf("the description names %q, which is the corpus", leak)
			}
		}
		want := []struct{ typ, path string }{
			{"text/markdown", "/search?q={searchTerms}&count={count?}&startIndex={startIndex?}"},
			{"text/html", "/search.html?q={searchTerms}&count={count?}&startIndex={startIndex?}"},
			{"application/json", "/search.json?q={searchTerms}&count={count?}&startIndex={startIndex?}"},
		}
		if len(d.Urls) != 4 {
			t.Fatalf("%d Url elements, want 4:\n%s", len(d.Urls), r.body)
		}
		for i, w := range want {
			u := d.Urls[i]
			if u.Type != w.typ || u.Template != "http://"+host+w.path || (u.Rel != "" && u.Rel != "results") || strings.Count(u.Template, "{searchTerms}") != 1 {
				t.Errorf("Url %d: %+v, want %s %s", i, u, w.typ, "http://"+host+w.path)
			}
		}
		if s := d.Urls[3]; s.Type != "application/opensearchdescription+xml" || s.Rel != "self" || s.Template != "http://"+host+"/opensearch.xml" {
			t.Errorf("self Url: %+v", s)
		}
		if !strings.Contains(r.body, "&amp;count=") {
			t.Errorf("an ampersand is not written as &amp;:\n%s", r.body)
		}
	}
}

func TestEveryHTMLPageAdvertisesTheDescriptionAndTheFormSubmitsToTheSearch(t *testing.T) {
	e := started(t)
	for _, p := range []string{"/search.html?q=tomatoes", "/search.html?q=zebra", "/index.html", "/search.html"} {
		body := must(t, e.get(p), 200).body
		if !strings.Contains(body, `<link rel="search" type="application/opensearchdescription+xml" title="cmarkfind" href="/opensearch.xml">`) {
			t.Errorf("%s has no discovery link:\n%.500s", p, body)
		}
		if !strings.Contains(body, `<form`) || !strings.Contains(body, `name="q"`) {
			t.Errorf("%s has no search form", p)
		}
	}
}
