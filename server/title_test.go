package server_test

import (
	"encoding/xml"
	"strings"
	"testing"
)

// The title setting (DR-0018 item 13): the name of the service, in every place
// the service names itself, so that a browser's "Add NAME" says which one.

func TestTheTitleNamesTheServiceWhereverItNamesItself(t *testing.T) {
	db, _ := mainCorpus(t)
	s := defaults()
	s.Title = "wren notes"
	e := start(t, db, s)

	var d osd
	if err := xml.Unmarshal([]byte(must(t, e.get("/opensearch.xml"), 200).body), &d); err != nil || d.ShortName != "wren notes" {
		t.Errorf("the description's ShortName is %q (%v), want the title", d.ShortName, err)
	}
	wantLink := `</opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="wren notes"`
	for _, p := range []string{"/search?q=tomatoes", "/search.json?q=tomatoes", "/nothing", "/opensearch.xml"} {
		if got := e.get(p).header.Get("Link"); got != wantLink {
			t.Errorf("%s: Link %q, want %q", p, got, wantLink)
		}
	}
	for _, p := range []string{"/index.html", "/search.html", "/search.html?q=tomatoes", "/sets.html"} {
		body := must(t, e.get(p), 200).body
		if !strings.Contains(body, `<link rel="search" type="application/opensearchdescription+xml" title="wren notes" href="/opensearch.xml">`) {
			t.Errorf("%s: the discovery link does not carry the title:\n%.400s", p, body)
		}
		if !strings.Contains(body, "<h1>wren notes</h1>") {
			t.Errorf("%s: no heading with the title", p)
		}
		if strings.Contains(body, "cmarkfind</h1>") || strings.Contains(body, `title="cmarkfind"`) {
			t.Errorf("%s still says cmarkfind where the title belongs", p)
		}
	}
	for _, p := range []string{"/", "/search"} {
		if body := must(t, e.get(p), 200).body; !strings.HasPrefix(body, "# wren notes\n") {
			t.Errorf("%s does not start with the title as its heading:\n%.120s", p, body)
		}
	}
	st := jsonOf(t, must(t, e.get("/stats.json"), 200))["settings"].(map[string]any)["title"].(map[string]any)
	if st["value"] != "wren notes" {
		t.Errorf("/stats title: %v", st)
	}
}

func TestWithNoTitleTheServiceIsCalledCmarkfind(t *testing.T) {
	db, _ := mainCorpus(t)
	e := start(t, db, defaults()) // the helper's settings set no title: the default applies
	var d osd
	xml.Unmarshal([]byte(must(t, e.get("/opensearch.xml"), 200).body), &d)
	if d.ShortName != "cmarkfind" {
		t.Errorf("ShortName %q", d.ShortName)
	}
	if got := e.get("/").header.Get("Link"); got != linkValue {
		t.Errorf("Link %q, want %q", got, linkValue)
	}
	if body := must(t, e.get("/index.html"), 200).body; !strings.Contains(body, "<h1>cmarkfind</h1>") {
		t.Errorf("heading:\n%.300s", body)
	}
}
