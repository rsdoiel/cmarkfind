package cli

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Phase 6c through the command: a set defines its facets with one set fields
// call; the Markdown shows them as links under their labels, an outer Facets
// list before an outer Results list, only when there are facets; the same facts
// are in -json; a facet link is a query; date fields are queried by name.

var datedPagesCLI = map[string]string{
	"p1.md": "---\ntitle: One\ndatePublished: 2026-03-01\ndateCreated: 2025-01-10\nkeywords: [Go, Oberon]\nauthor: Ann\n---\ncompilers one\n",
	"p2.md": "---\ntitle: Two\ndatePublished: 2026-04-02\ndateCreated: 2026-04-01\nkeywords: [go]\nauthor: Ann\n---\ncompilers two\n",
	"p3.md": "---\ntitle: Three\ndatePublished: sometime\ndateCreated: 2025-12-31\nkeywords: \"go, wasm\"\nauthor: Bob\n---\ncompilers three\n",
	"p4.md": "# Four\n\ncompilers four\n",
}

// facetArgs is the one set fields call that defines every facet of the set.
var facetArgs = []string{`datePublished=date,facet,"Pub Date"`, "dateCreated=date,facet", "keywords=facet,fielded,Topics", "author=facet,fielded"}

func datedSet(t *testing.T) (db, set string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	root := hostSite(t, "f.example", datedPagesCLI)
	db = dbPath(t)
	if r := do(t, "-db", db, "fill", root); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	return db, root
}

func facetedSet(t *testing.T) (db, set string) {
	t.Helper()
	db, set = datedSet(t)
	if r := do(t, append([]string{"-db", db, "set", "fields", set}, facetArgs...)...); r.code != 0 {
		t.Fatalf("set fields: %+v", r)
	}
	return db, set
}

func TestAFacetIsOptInSoASetWithNoFacetsHasTheFlatList(t *testing.T) {
	db, _ := datedSet(t)
	r := do(t, "-db", db, "search", "compilers")
	if r.code != 0 || !strings.HasPrefix(r.out, "- [") || strings.Contains(r.out, "Facets") || strings.Contains(r.out, "Results") {
		t.Errorf("the flat list:\n%s", r.out)
	}
	j := parseJSON(t, do(t, "-db", db, "search", "-json", "compilers").out)
	if f, ok := j["facets"].(map[string]any); !ok || len(f) != 0 {
		t.Errorf("facets = %v", j["facets"])
	}
}

func TestTheMarkdownHasAFacetsListUnderLabelsBeforeAResultsList(t *testing.T) {
	db, _ := facetedSet(t)
	r := do(t, "-db", db, "search", "compilers")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	lines := strings.Split(strings.TrimRight(r.out, "\n"), "\n")
	wantStart := []string{
		"- Facets",
		"  - author", "    - [ann (2)](", "    - [bob (1)](",
		"  - dateCreated", "    - [2025 (2)](", "    - [2026 (1)](",
		"  - Pub Date", "    - [2026 (2)](",
		"  - Topics", "    - [go (3)](", "    - [oberon (1)](", "    - [wasm (1)](",
		"- Results",
	}
	if len(lines) < len(wantStart) {
		t.Fatalf("output too short:\n%s", r.out)
	}
	for i, w := range wantStart {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("line %d = %q, want it to start %q\n%s", i, lines[i], w, r.out)
			break
		}
	}
	var hitLines, detail int
	for _, l := range lines[len(wantStart):] {
		if strings.HasPrefix(l, "  - [") {
			hitLines++
		} else if strings.HasPrefix(l, "    ") {
			detail++
		} else {
			t.Errorf("a line outside the Results list: %q", l)
		}
	}
	if hitLines != 4 || detail < 4 {
		t.Errorf("%d hits and %d detail lines in:\n%s", hitLines, detail, r.out)
	}
	if !strings.Contains(r.out, "  - [One](https://f.example/p1.md)\n    own\n") {
		t.Errorf("a hit's link and detail:\n%s", r.out)
	}
}

var facetLink = regexp.MustCompile(`\[([^\]]+) \((\d+)\)\]\((\?q=[^)]*)\)`)

// A facet link is a query: ?q= and the query encoded. Run it and it finds the
// number of pages its label says.
func TestEveryFacetLinkRunsAsAQueryAndFindsWhatItsLabelCounts(t *testing.T) {
	db, _ := facetedSet(t)
	r := do(t, "-db", db, "search", "compilers")
	links := facetLink.FindAllStringSubmatch(r.out, -1)
	if len(links) != 8 {
		t.Fatalf("%d facet links in:\n%s", len(links), r.out)
	}
	for _, m := range links {
		label, count, target := m[1], m[2], m[3]
		if !strings.HasPrefix(target, "?q=") || strings.Contains(target, " ") {
			t.Errorf("%s: target %q is not an encoded query", label, target)
		}
		q, err := url.QueryUnescape(strings.TrimPrefix(target, "?q="))
		if err != nil {
			t.Fatal(err)
		}
		got := addrs(t, db, "search", q)
		if n, _ := strconv.Atoi(count); len(got) != n {
			t.Errorf("%s (%s): %q finds %d pages: %v", label, count, q, len(got), got)
		}
	}
}

func TestSearchSetFacetLinksNameTheSet(t *testing.T) {
	db, set := facetedSet(t)
	r := do(t, "-db", db, "search-set", set, "compilers")
	links := facetLink.FindAllStringSubmatch(r.out, -1)
	if r.code != 0 || len(links) == 0 {
		t.Fatalf("%+v", r)
	}
	for _, m := range links {
		if !strings.Contains(m[3], "&set="+url.QueryEscape(set)) {
			t.Errorf("%s: %q does not name the set", m[1], m[3])
		}
	}
}

func TestJSONHasEachFacetByFieldWithItsLabelAndValues(t *testing.T) {
	db, _ := facetedSet(t)
	j := parseJSON(t, do(t, "-db", db, "search", "-json", "compilers").out)
	facets, ok := j["facets"].(map[string]any)
	if !ok || len(facets) != 4 {
		t.Fatalf("facets = %v", j["facets"])
	}
	kw := facets["keywords"].(map[string]any)
	if kw["label"] != "Topics" {
		t.Errorf("label %v", kw["label"])
	}
	vals, _ := kw["values"].([]any)
	if len(vals) != 3 {
		t.Fatalf("values %v", kw["values"])
	}
	first := vals[0].(map[string]any)
	if first["value"] != "go" || first["count"] != float64(3) || first["query"] != `(compilers) AND keywords:"go"` {
		t.Errorf("first value %v", first)
	}
	pub := facets["datepublished"].(map[string]any)
	pv, _ := pub["values"].([]any)
	if pub["label"] != "Pub Date" || len(pv) != 1 || pv[0].(map[string]any)["query"] != "(compilers) AND datepublished:[2026 TO 2026]" {
		t.Errorf("datepublished = %v", pub)
	}
	if hs, _ := j["hits"].([]any); len(hs) != 4 {
		t.Errorf("hits %v", j["hits"])
	}
}

func TestDateQueriesNameTheDateField(t *testing.T) {
	db, _ := datedSet(t) // no facet needed
	for q, want := range map[string][]string{
		"datepublished:2026":                 {"p1.md", "p2.md"},
		"datePublished:[2026-03 TO 2026-04]": {"p1.md", "p2.md"},
		"datepublished:{2026-03 TO 2026-05}": {"p2.md"}, // after March, up to and including April
		"datecreated:[2025 TO 2026}":         {"p1.md", "p3.md"},
		"compilers -datepublished:2026":      {"p3.md", "p4.md"},
		"date:2026":                          nil,
	} {
		if got := addrs(t, db, "search", q); !same(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}

func TestSetFieldsShowsTheLabelsAndSetShowListsTheFacets(t *testing.T) {
	db, set := facetedSet(t)
	r := do(t, "-db", db, "set", "fields", set)
	out := strings.Join(strings.Fields(r.out), " ")
	for _, want := range []string{`datepublished facet,date "Pub Date"`, `keywords facet,fielded "Topics"`, `author facet,fielded "author"`, `datecreated facet,date "dateCreated"`} {
		if !strings.Contains(out, want) {
			t.Errorf("set fields lacks %q:\n%s", want, r.out)
		}
	}
	if !strings.Contains(r.out, "title") || strings.Contains(r.out, `title built-in "`) {
		t.Errorf("a field with no facet has no label:\n%s", r.out)
	}
	s := do(t, "-db", db, "set", "show", set)
	for _, want := range []string{"facets", "Pub Date (datepublished)", "Topics (keywords)"} {
		if !strings.Contains(s.out, want) {
			t.Errorf("set show lacks %q:\n%s", want, s.out)
		}
	}
	j := parseJSON(t, do(t, "-db", db, "set", "show", "-json", set).out)
	fs, _ := j["facets"].([]any)
	if len(fs) != 4 {
		t.Fatalf("facets %v", j["facets"])
	}
	found := false
	for _, f := range fs {
		m := f.(map[string]any)
		if m["field"] == "datepublished" && m["label"] == "Pub Date" && m["date"] == true {
			found = true
		}
	}
	if !found {
		t.Errorf("facets %v", fs)
	}
	jf := parseJSON(t, do(t, "-db", db, "set", "fields", "-json", set).out)
	if fl, _ := jf["facets"].([]any); len(fl) != 4 {
		t.Errorf("set fields -json facets %v", jf["facets"])
	}
	if no := parseJSON(t, do(t, "-db", db, "set", "show", "-json", set).out); no["fields"] != "custom" {
		t.Errorf("fields = %v", no["fields"])
	}
}

func TestABadLabelIsExit2(t *testing.T) {
	db, set := datedSet(t)
	for _, bad := range []string{`author=fielded,Writers`, `author=facet,"Writers`, `author=facet,""`, `author=facet,A,B`} {
		if r := do(t, "-db", db, "set", "fields", set, bad); r.code != 2 {
			t.Errorf("%s: %+v", bad, r)
		}
	}
}

func TestAListingWithNoQueryHasNoFacets(t *testing.T) {
	db, _ := facetedSet(t)
	do(t, "-db", db, "tag", "https://f.example/p1.md", "x")
	r := do(t, "-db", db, "search", "-tag", "x")
	if r.code != 0 || strings.Contains(r.out, "Facets") {
		t.Errorf("%+v", r)
	}
}
