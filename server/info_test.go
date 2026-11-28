package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/server"
)

// ---- the information endpoints ---------------------------------------------------------------

func TestVersionSaysTheProgramsVersionAndHashInThreeFormats(t *testing.T) {
	e := started(t)
	j := jsonOf(t, must(t, e.get("/version.json"), 200))
	if j["name"] != "cmarkfind" || j["version"] != "9.8.7" || j["release_date"] != "2026-11-27" || j["hash"] != "abc1234" {
		t.Errorf("version: %v", j)
	}
	for _, p := range []string{"/version", "/version.html"} {
		b := must(t, e.get(p), 200).body
		for _, w := range []string{"9.8.7", "abc1234", "2026-11-27"} {
			if !strings.Contains(b, w) {
				t.Errorf("%s lacks %q:\n%s", p, w, b)
			}
		}
	}
	if got := must(t, e.get("/version", "Accept", "application/json"), 200).header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Accept json: %s", got)
	}
}

func TestStatsReportsTheSettingsWhatWasStoredWhatTheServerHasDoneAndTheCorpusInNumbers(t *testing.T) {
	db, _ := mainCorpus(t)
	s := defaults()
	s.Results = 50
	s.Concurrency = 3
	e := start(t, db, s, func(sv *server.Server) { sv.Stored = map[string]bool{"results": true} })
	e.get("/search?q=tomatoes")
	e.get("/search.json?q=tomatoes")
	e.get("/nothing")
	e.get("/search?q=" + strings.Repeat("a", 1025))
	j := jsonOf(t, must(t, e.get("/stats.json"), 200))
	set := j["settings"].(map[string]any)
	for name, want := range map[string]struct {
		value  any
		stored bool
	}{"results": {float64(50), true}, "concurrency": {float64(3), false}, "info": {true, false}, "css": {"", false}} {
		got, ok := set[name].(map[string]any)
		if !ok || got["value"] != want.value || got["stored"] != want.stored {
			t.Errorf("setting %s: %v, want value %v stored %v", name, set[name], want.value, want.stored)
		}
	}
	if _, ok := set["port"].(map[string]any); !ok {
		t.Errorf("no port setting: %v", set)
	}
	req := j["requests"].(map[string]any)
	if req["2xx"].(float64) < 2 || req["4xx"].(float64) < 2 || req["5xx"] != float64(0) {
		t.Errorf("requests by class: %v", req)
	}
	if j["searches"].(float64) != 2 || j["in_flight"].(float64) != 0 || j["busy"] != float64(0) {
		t.Errorf("searches %v, in_flight %v, busy %v", j["searches"], j["in_flight"], j["busy"])
	}
	if _, ok := j["uptime_seconds"].(float64); !ok || j["started"] == nil {
		t.Errorf("no uptime or start time: %v", j)
	}
	c := j["corpus"].(map[string]any)
	if c["sets"] != float64(1) || c["pages"] != float64(250) || c["local"] != float64(1) || c["mirror"] != float64(0) || c["reference"] != float64(0) {
		t.Errorf("corpus: %v", c)
	}
	// the counters move
	e.get("/search?q=tomatoes")
	again := jsonOf(t, must(t, e.get("/stats.json"), 200))
	if again["searches"].(float64) != 3 {
		t.Errorf("searches after one more: %v", again["searches"])
	}
	for _, p := range []string{"/stats", "/stats.html"} {
		b := must(t, e.get(p), 200).body
		for _, w := range []string{"concurrency", "results", "250"} {
			if !strings.Contains(b, w) {
				t.Errorf("%s lacks %q", p, w)
			}
		}
	}
}

func TestSetsListsEachSetWithTheNameSetTakesAndNothingElse(t *testing.T) {
	db, root := mainCorpus(t)
	other := writeSite(t, "b.example", map[string]string{"x.md": "# X\n\ntomatoes\n"})
	if _, err := db.FillLocal(ctxT(t), other, tocLimits()); err != nil {
		t.Fatal(err)
	}
	if err := db.IgnoreSet(context.Background(), other, true); err != nil {
		t.Fatal(err)
	}
	e := start(t, db, defaults())
	j := jsonOf(t, must(t, e.get("/sets.json"), 200))
	byName := map[string]map[string]any{}
	for _, s := range j["sets"].([]any) {
		m := s.(map[string]any)
		byName[m["name"].(string)] = m
		for k := range m {
			if k != "name" && k != "source" && k != "pages" && k != "ignored" && k != "built" {
				t.Errorf("a set lists %q, which the record does not offer", k)
			}
		}
	}
	if len(byName) != 2 {
		t.Fatalf("%d sets: %v", len(byName), j)
	}
	if m := byName[root]; m == nil || m["source"] != "local" || m["pages"] != float64(250) || m["ignored"] != false || m["built"] != true {
		t.Errorf("main set: %v", m)
	}
	if m := byName[other]; m == nil || m["ignored"] != true || m["pages"] != float64(1) {
		t.Errorf("other set: %v", m)
	}
	// the name given is the name set= takes
	for name := range byName {
		if r := e.get("/search.json?q=tomatoes&set=" + queryEscape(name)); r.status != 200 {
			t.Errorf("set=%s: %d", name, r.status)
		}
	}
	for _, p := range []string{"/sets", "/sets.html"} {
		if b := must(t, e.get(p), 200).body; !strings.Contains(b, "b.example") && !strings.Contains(b, "local") {
			t.Errorf("%s:\n%s", p, b)
		}
	}
}

func TestWithInfoOffTheThreeEndpointsAreNotFoundAndTheRootDoesNotLinkThem(t *testing.T) {
	db, _ := mainCorpus(t)
	on := start(t, db, defaults())
	for _, l := range []string{"/version", "/stats", "/sets"} {
		if !strings.Contains(must(t, on.get("/"), 200).body, l) {
			t.Errorf("with info on, the root page does not mention %s", l)
		}
	}
	s := defaults()
	s.Info = false
	off := start(t, db, s)
	for _, p := range []string{"/version", "/version.json", "/stats", "/stats.html", "/sets", "/sets.json"} {
		if got := off.get(p).status; got != 404 {
			t.Errorf("info off, %s: %d, want 404", p, got)
		}
	}
	root := must(t, off.get("/"), 200).body
	for _, l := range []string{"/version", "/stats", "/sets"} {
		if strings.Contains(root, l) {
			t.Errorf("info off, the root page still mentions %s", l)
		}
	}
	if got := off.get("/search.json?q=tomatoes").status; got != 200 {
		t.Errorf("info off must leave the search alone: %d", got)
	}
	if got := off.get("/opensearch.xml").status; got != 200 {
		t.Errorf("info off must leave the description alone: %d", got)
	}
}

// ---- style ------------------------------------------------------------------------------------------

func TestTheStylesheetSettingAddsOneLinkToHTMLAndItsOriginToThePolicyAndNothingElse(t *testing.T) {
	db, _ := mainCorpus(t)
	for _, tc := range []struct{ css, link, style string }{
		{"https://static.example/site.css", `<link rel="stylesheet" href="https://static.example/site.css">`, "style-src https://static.example"},
		{"/style.css", `<link rel="stylesheet" href="/style.css">`, "style-src 'self'"},
		{"http://localhost:9999/a.css", `<link rel="stylesheet" href="http://localhost:9999/a.css">`, "style-src http://localhost:9999"},
	} {
		s := defaults()
		s.CSS = tc.css
		e := start(t, db, s)
		html := must(t, e.get("/search.html?q=tomatoes"), 200)
		if !strings.Contains(html.body, tc.link) {
			t.Errorf("css %s: no link in\n%.500s", tc.css, html.body)
		}
		if csp := html.header.Get("Content-Security-Policy"); !strings.Contains(csp, tc.style) || strings.Count(csp, "style-src") != 1 {
			t.Errorf("css %s: policy %q, want %q once", tc.css, csp, tc.style)
		}
		for _, p := range []string{"/index.html", "/search.html"} {
			if !strings.Contains(must(t, e.get(p), 200).body, tc.link) {
				t.Errorf("css %s: %s has no link", tc.css, p)
			}
		}
		for _, p := range []string{"/search?q=tomatoes", "/search.json?q=tomatoes", "/opensearch.xml"} {
			if strings.Contains(must(t, e.get(p), 200).body, "stylesheet") {
				t.Errorf("css %s: %s mentions a stylesheet", tc.css, p)
			}
		}
	}
	none := start(t, db, defaults())
	html := must(t, none.get("/search.html?q=tomatoes"), 200)
	if strings.Contains(html.body, "stylesheet") || strings.Contains(html.body, "<style") || strings.Contains(html.header.Get("Content-Security-Policy"), "style-src") {
		t.Error("with no css setting the page names a style")
	}
}

// ---- the stored settings -------------------------------------------------------------------------------

func TestSettingsReadAsDefaultsUntilStoredAndAreStoredByNameAndRemovedByDefault(t *testing.T) {
	db := openCorpus(t)
	ctx := context.Background()
	s, stored, err := server.ReadSettings(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 8900 || s.Concurrency < 2 || s.Results != 100 || s.CSS != "" || !s.Info || len(stored) != 0 {
		t.Errorf("defaults: %+v stored %v", s, stored)
	}
	for name, value := range map[string]string{"port": "8123", "concurrency": "7", "results": "25", "css": "https://x.example/a.css", "info": "off"} {
		if err := server.WriteSetting(ctx, db, name, value); err != nil {
			t.Errorf("set %s %s: %v", name, value, err)
		}
	}
	s, stored, _ = server.ReadSettings(ctx, db)
	if s.Port != 8123 || s.Concurrency != 7 || s.Results != 25 || s.CSS != "https://x.example/a.css" || s.Info || len(stored) != 5 {
		t.Errorf("stored: %+v %v", s, stored)
	}
	for _, name := range server.SettingNames {
		if err := server.WriteSetting(ctx, db, name, "default"); err != nil {
			t.Errorf("default %s: %v", name, err)
		}
	}
	if s, stored, _ = server.ReadSettings(ctx, db); s.Port != 8900 || s.Results != 100 || !s.Info || len(stored) != 0 {
		t.Errorf("after default: %+v %v", s, stored)
	}
	if err := server.WriteSetting(ctx, db, "port", "0"); err != nil { // a stored 0 is a free port at each run
		t.Fatal(err)
	}
	if s, stored, _ = server.ReadSettings(ctx, db); s.Port != 0 || !stored["port"] {
		t.Errorf("a stored port 0: %+v %v", s, stored)
	}
}

func TestEveryBadSettingIsRefusedAndNothingBadIsStored(t *testing.T) {
	db := openCorpus(t)
	ctx := context.Background()
	// a good value is taken, so the refusals below are the validation and not a store that fails
	if err := server.WriteSetting(ctx, db, "port", "8123"); err != nil {
		t.Fatalf("a valid port was refused: %v", err)
	}
	for _, c := range [][2]string{
		{"port", "-1"}, {"port", "65536"}, {"port", "abc"}, {"port", ""}, {"port", "80.5"},
		{"concurrency", "0"}, {"concurrency", "257"}, {"concurrency", "-2"}, {"concurrency", "many"},
		{"results", "0"}, {"results", "1001"}, {"results", "-1"}, {"results", "lots"},
		{"css", "ftp://x.example/a.css"}, {"css", "//x.example/a.css"}, {"css", "style.css"}, {"css", "https://x.example/a b.css"},
		{"css", "https://x.example/a.css\n"}, {"css", "javascript:alert(1)"}, {"css", "https://"}, {"css", "/" + strings.Repeat("a", 2048)},
		{"info", "yes"}, {"info", ""}, {"info", "ON "}, {"info", "1"},
		{"colour", "red"}, {"", "x"}, {"Port", "9000"},
	} {
		if err := server.WriteSetting(ctx, db, c[0], c[1]); err == nil {
			t.Errorf("%q = %q was accepted", c[0], c[1])
		}
	}
	got, stored, err := server.ReadSettings(ctx, db)
	if err != nil || len(stored) != 1 || !stored["port"] || got.Port != 8123 {
		t.Errorf("only the valid port should be stored: %+v %v %v", got, stored, err)
	}
}
