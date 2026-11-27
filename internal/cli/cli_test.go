package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/corpus"
)

type run struct {
	code        int
	out, errOut string
}

func do(t *testing.T, args ...string) run {
	t.Helper()
	var o, e bytes.Buffer
	code := Run(args, &o, &e)
	return run{code, o.String(), e.String()}
}

// localSite writes three pages and an outline that lists them by published address.
func localSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pages := map[string]string{
		"a.md": "---\ntitle: Otters\n---\nThe otter swims in the river.\n",
		"b.md": "# Badgers\n\nBadgers dig setts in the woods.\n",
		"c.md": "# Cranes\n\nA crane stands in the shallow water.\n",
	}
	var outlines string
	for _, n := range []string{"a.md", "b.md", "c.md"} {
		os.WriteFile(filepath.Join(root, n), []byte(pages[n]), 0o644)
		outlines += fmt.Sprintf(`<outline text="%s" url="https://example.org/%s"/>`, n, n)
	}
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"),
		[]byte(`<?xml version="1.0"?><opml version="2.0"><head/><body>`+outlines+`</body></opml>`), 0o644)
	return root
}

func dbPath(t *testing.T) string { return filepath.Join(t.TempDir(), "sub", "corpus.db") }

func filled(t *testing.T) string {
	t.Helper()
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", localSite(t)); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	return db
}

// webSite serves the three pages and an outline over HTTP; the paths in bad
// answer 500.
func webSite(t *testing.T, bad ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, b := range bad {
			if r.URL.Path == b {
				http.Error(w, "boom", 500)
				return
			}
		}
		switch r.URL.Path {
		case "/.well-known/toc.opml":
			fmt.Fprintf(w, `<opml version="2.0"><head/><body><outline text="a" url="http://%[1]s/a.md"/><outline text="b" url="http://%[1]s/b.md"/><outline text="c" url="http://%[1]s/c.md"/></body></opml>`, r.Host)
		case "/a.md", "/b.md", "/c.md":
			w.Header().Set("Content-Type", "text/markdown")
			fmt.Fprintf(w, "# Page %s\n\nBadgers dig setts.\n", r.URL.Path[1:2])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func parseJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, s)
	}
	return m
}

func TestFillALocalSiteThenAgainTheSecondIsAllUnchanged(t *testing.T) {
	db, site := dbPath(t), localSite(t)
	r := do(t, "-db", db, "fill", site)
	if r.code != 0 || !strings.Contains(r.out, "added 3") {
		t.Fatalf("%+v", r)
	}
	r = do(t, "-db", db, "fill", site)
	if r.code != 0 || !strings.Contains(r.out, "unchanged 3") || !strings.Contains(r.out, "added 0") {
		t.Fatalf("second fill: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "fill", "-json", site).out)
	if j["unchanged"] != float64(3) || j["complete"] != true {
		t.Errorf("json %v", j)
	}
}

func TestFillARemoteSiteByItsRootOrItsOutline(t *testing.T) {
	srv := webSite(t)
	db := dbPath(t)
	r := do(t, "-db", db, "fill", "-pause", "0s", srv.URL)
	if r.code != 0 || !strings.Contains(r.out, "added 3") {
		t.Fatalf("by root: %+v", r)
	}
	r = do(t, "-db", db, "fill", "-pause", "0s", srv.URL+"/.well-known/toc.opml")
	if r.code != 0 || !strings.Contains(r.out, "unchanged 3") {
		t.Fatalf("by outline: %+v", r)
	}
}

func TestFillWithAFailingPageFinishesThenExitsWithItsClass(t *testing.T) {
	srv := webSite(t, "/b.md")
	db := dbPath(t)
	r := do(t, "-db", db, "fill", "-pause", "0s", srv.URL)
	if r.code != 69 || !strings.Contains(r.out, "added 2") || !strings.Contains(r.out, "failed 1") {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(r.errOut, "cmarkfind: ") {
		t.Errorf("stderr %q", r.errOut)
	}
	if s := do(t, "-db", db, "search", "badgers"); s.code != 0 {
		t.Errorf("the pages that worked are searchable: %+v", s)
	}
}

func TestSearchPrintsHitsAndTheCountGoesToStandardError(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "search", "badgers")
	if r.code != 0 || !strings.Contains(r.out, "Badgers") || !strings.Contains(r.out, "https://example.org/b.md") || !strings.Contains(r.out, "own") {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.out, "searched") || !strings.Contains(r.errOut, "1 hit") || !strings.Contains(r.errOut, "searched 3") {
		t.Errorf("count should be on stderr: out %q err %q", r.out, r.errOut)
	}
	r = do(t, "-db", db, "search", "-limit", "1", "the")
	if strings.Count(r.out, "https://example.org/") != 1 {
		t.Errorf("limit: %q", r.out)
	}
	if r = do(t, "-db", db, "search", `"shallow`, "water"); r.code != 0 { // unbalanced quote is plain words, not an error
		t.Errorf("%+v", r)
	}
}

func TestSearchWithNoMatchExitsOneAndStaysValidJSON(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "search", "-json", "zebra")
	if r.code != 1 {
		t.Fatalf("code %d", r.code)
	}
	j := parseJSON(t, r.out)
	if h, ok := j["hits"].([]any); !ok || len(h) != 0 || j["searched"] != float64(3) {
		t.Errorf("json %v", j)
	}
	j = parseJSON(t, do(t, "-db", db, "search", "-json", "badgers").out)
	h := j["hits"].([]any)[0].(map[string]any)
	if h["address"] != "https://example.org/b.md" || h["tier"] != "own" {
		t.Errorf("hit %v", h)
	}
}

// The schema version is the library's (3 since cmarkfind DR-0004); this test
// pinned 1, then 2.
func TestStats(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "stats")
	if r.code != 0 || !strings.Contains(r.out, "pages 3") || !strings.Contains(r.out, "own") {
		t.Fatalf("%+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "stats", "-json").out)
	if j["pages"] != float64(3) || j["gone"] != float64(0) || j["schema_version"] != float64(corpus.SchemaVersion) || j["path"] != db {
		t.Errorf("json %v", j)
	}
	if sites, _ := j["sites"].([]any); len(sites) != 1 {
		t.Errorf("sites %v", j["sites"])
	}
}

func TestExitCodes(t *testing.T) {
	good := filled(t)
	notDB := filepath.Join(t.TempDir(), "x.db")
	os.WriteFile(notDB, []byte(strings.Repeat("not a database ", 500)), 0o644)
	blocker := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(blocker, []byte("x"), 0o644)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	noOutline := t.TempDir()
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no verb", nil, 2},
		{"unknown verb", []string{"frobnicate"}, 2},
		{"unknown option", []string{"-db", good, "search", "-nope", "x"}, 2},
		{"fill without source", []string{"-db", good, "fill"}, 2},
		{"search without query", []string{"-db", good, "search"}, 2},
		{"bad limit", []string{"-db", good, "search", "-limit", "x", "word"}, 2},
		{"zero limit", []string{"-db", good, "search", "-limit", "0", "word"}, 2},
		{"bad address", []string{"-db", good, "fill", "http://"}, 2},
		{"stats takes no arguments", []string{"-db", good, "stats", "extra"}, 2},
		{"search missing database", []string{"-db", filepath.Join(t.TempDir(), "none.db"), "search", "x"}, 66},
		{"stats missing database", []string{"-db", filepath.Join(t.TempDir(), "none.db"), "stats"}, 66},
		{"fill missing folder", []string{"-db", dbPath(t), "fill", filepath.Join(t.TempDir(), "nope")}, 66},
		{"fill folder without outline", []string{"-db", dbPath(t), "fill", noOutline}, 66},
		{"fill site with no outline", []string{"-db", dbPath(t), "fill", "-pause", "0s", webSite(t).URL + "/nowhere/toc.opml"}, 66},
		{"fill unreachable", []string{"-db", dbPath(t), "fill", "-pause", "0s", dead.URL}, 69},
		{"database that is not a corpus", []string{"-db", notDB, "search", "x"}, 65},
		{"database cannot be created", []string{"-db", filepath.Join(blocker, "x", "c.db"), "fill", localSite(t)}, 73},
		{"search finds nothing", []string{"-db", good, "search", "zebra"}, 1},
		{"help", []string{"-help"}, 0},
		{"version", []string{"-version"}, 0},
		{"license", []string{"-license"}, 0},
		{"help verb", []string{"help", "search"}, 0},
		{"help unknown verb", []string{"help", "nope"}, 2},
	}
	for _, c := range cases {
		if r := do(t, c.args...); r.code != c.want {
			t.Errorf("%s: exit %d, want %d\nstdout %q\nstderr %q", c.name, r.code, c.want, r.out, r.errOut)
		}
	}
}

func TestANewerSchemaIsExit65(t *testing.T) {
	db := filled(t)
	if err := setSchema(db, "999"); err != nil {
		t.Fatal(err)
	}
	if r := do(t, "-db", db, "stats"); r.code != 65 {
		t.Errorf("%+v", r)
	}
}

func TestAFailureInJSONModeIsAlsoAJSONObjectOnStandardOutput(t *testing.T) {
	r := do(t, "-db", filepath.Join(t.TempDir(), "none.db"), "search", "-json", "x")
	if r.code != 66 {
		t.Fatalf("%+v", r)
	}
	j := parseJSON(t, r.out)
	if j["class"] != "no_input" || j["code"] != float64(66) || j["error"] == "" {
		t.Errorf("json %v", j)
	}
	if !strings.HasPrefix(r.errOut, "cmarkfind: ") || strings.Count(r.errOut, "\n") != 1 {
		t.Errorf("stderr %q", r.errOut)
	}
}

func TestTheDatabaseIsFoundFromTheEnvironmentAndDashDBWins(t *testing.T) {
	envDB := filepath.Join(t.TempDir(), "deep", "er", "env.db")
	t.Setenv("CMARKFIND_DB", envDB)
	if r := do(t, "fill", localSite(t)); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(envDB); err != nil {
		t.Fatalf("the environment's file was not made: %v", err)
	}
	other := dbPath(t)
	do(t, "-db", other, "fill", localSite(t))
	if j := parseJSON(t, do(t, "-db", other, "stats", "-json").out); j["path"] != other {
		t.Errorf("-db did not win: %v", j["path"])
	}
}

func TestTheDefaultIsInTheUserDataDirectory(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CMARKFIND_DB", "")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("HOME", data) // for platforms that do not read XDG_DATA_HOME
	if got := DefaultDB(); !strings.HasPrefix(got, data) || filepath.Base(got) != "corpus.db" || filepath.Base(filepath.Dir(got)) != "cmarkfind" {
		t.Errorf("DefaultDB() = %q", got)
	}
}

func TestTheBinaryExitsWithTheCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	bin := filepath.Join(t.TempDir(), "cmarkfind")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/cmarkfind")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	code := func(args ...string) int {
		err := exec.Command(bin, args...).Run()
		if err == nil {
			return 0
		}
		return err.(*exec.ExitError).ExitCode()
	}
	db := filled(t)
	for args, want := range map[string]int{
		"-version":                      0,
		"frobnicate":                    2,
		"-db " + db + " search zebra":   1,
		"-db " + db + " search badgers": 0,
		"-db /nonexistent/x.db stats":   66,
	} {
		if got := code(strings.Fields(args)...); got != want {
			t.Errorf("%s: exit %d, want %d", args, got, want)
		}
	}
}
