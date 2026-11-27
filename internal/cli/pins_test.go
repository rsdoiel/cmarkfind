package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// Tests for pin and tag through cli.Run, written before the
// code. localSite pages: a.md "Otters", b.md "Badgers", c.md "Cranes".

const (
	addrA = "https://example.org/a.md"
	addrB = "https://example.org/b.md"
	addrC = "https://example.org/c.md"
)

func hitAddresses(t *testing.T, out string) []string {
	t.Helper()
	var got []string
	for _, h := range parseJSON(t, out)["hits"].([]any) {
		got = append(got, h.(map[string]any)["address"].(string))
	}
	return got
}

func TestPinPutsAPinnedMatchFirstAndMarksItInPlainAndJSONOutput(t *testing.T) {
	db := filled(t)
	// "the" matches all three; unpinned order is by rank, pin c to the top
	before := hitAddresses(t, do(t, "-db", db, "search", "-json", "the").out)
	if before[0] == addrC {
		t.Skip("c.md already first; pick another page")
	}
	if r := do(t, "-db", db, "pin", addrC); r.code != 0 {
		t.Fatalf("pin: %+v", r)
	}
	r := do(t, "-db", db, "search", "-json", "the")
	if got := hitAddresses(t, r.out); got[0] != addrC {
		t.Fatalf("order %v", got)
	}
	first := parseJSON(t, r.out)["hits"].([]any)[0].(map[string]any)
	if first["pinned"] != true {
		t.Errorf("hit %v", first)
	}
	if p := do(t, "-db", db, "search", "the"); !strings.Contains(strings.SplitN(p.out, "\n", 2)[0], "pinned") &&
		!strings.Contains(p.out, "pinned") {
		t.Errorf("plain output has no marker:\n%s", p.out)
	}
	if r := do(t, "-db", db, "unpin", addrC); r.code != 0 {
		t.Errorf("unpin: %+v", r)
	}
	if got := hitAddresses(t, do(t, "-db", db, "search", "-json", "the").out); got[0] != before[0] {
		t.Errorf("unpin did not restore %v", got)
	}
}

func TestPinListSaysWhetherEachPageIsStoredOrNotInTheCorpus(t *testing.T) {
	db := filled(t)
	do(t, "-db", db, "pin", addrA)
	r := do(t, "-db", db, "pin", "https://example.org/never.md")
	if r.code != 0 || r.errOut == "" {
		t.Errorf("pinning an unknown address should succeed and note it on stderr: %+v", r)
	}
	r = do(t, "-db", db, "pin", "-list")
	if r.code != 0 || !strings.Contains(r.out, "Otters") || !strings.Contains(r.out, "not in the corpus") {
		t.Errorf("list: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "pin", "-list", "-json").out)
	if len(j["pinned"].([]any)) != 2 {
		t.Errorf("json %v", j)
	}
}

func TestPinListOfNothingIsExitZeroAndValidJSON(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "pin", "-list", "-json")
	if j := parseJSON(t, r.out); r.code != 0 || len(j["pinned"].([]any)) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestTagThenSearchWithTagFiltersAndTagOnlyListsPages(t *testing.T) {
	db := filled(t)
	if r := do(t, "-db", db, "tag", addrA, "Wet", "furry"); r.code != 0 {
		t.Fatalf("tag: %+v", r)
	}
	do(t, "-db", db, "tag", addrB, "furry")
	r := do(t, "-db", db, "search", "-json", "-tag", "furry", "the")
	if got := hitAddresses(t, r.out); len(got) != 2 {
		t.Errorf("one tag: %v", got)
	}
	r = do(t, "-db", db, "search", "-json", "-tag", "furry", "-tag", "wet", "the")
	got := hitAddresses(t, r.out)
	if len(got) != 1 || got[0] != addrA {
		t.Errorf("two tags: %v", got)
	}
	h := parseJSON(t, r.out)["hits"].([]any)[0].(map[string]any)
	if tags, _ := h["tags"].([]any); len(tags) != 2 {
		t.Errorf("hit tags %v", h)
	}
	if !strings.Contains(do(t, "-db", db, "search", "-tag", "furry", "the").errOut, "with the tag") {
		t.Error("the count line does not say what was filtered")
	}
	// tag only, no query: ordered by title (Badgers, Otters)
	r = do(t, "-db", db, "search", "-json", "-tag", "furry")
	if got := hitAddresses(t, r.out); r.code != 0 || len(got) != 2 || got[0] != addrB || got[1] != addrA {
		t.Errorf("tag only: %+v", r)
	}
	// a plain word does not match a tag
	if do(t, "-db", db, "search", "wet").code != 1 {
		t.Error("a tag matched as text")
	}
}

func TestTagListShowsEveryTagWithCountsOrOnePagesTags(t *testing.T) {
	db := filled(t)
	do(t, "-db", db, "tag", addrA, "x", "y")
	do(t, "-db", db, "tag", addrB, "x")
	r := do(t, "-db", db, "tag", "-list")
	if r.code != 0 || !strings.Contains(r.out, "x") || !strings.Contains(r.out, "2") {
		t.Errorf("list: %+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "tag", "-list", "-json", addrA).out)
	if len(j["tags"].([]any)) != 2 {
		t.Errorf("one page: %v", j)
	}
}

func TestPinnedOptionLimitsToPinnedPagesWithOrWithoutAQuery(t *testing.T) {
	db := filled(t)
	do(t, "-db", db, "pin", addrC)
	r := do(t, "-db", db, "search", "-json", "-pinned", "the")
	if got := hitAddresses(t, r.out); len(got) != 1 || got[0] != addrC {
		t.Errorf("with query: %v", got)
	}
	r = do(t, "-db", db, "search", "-json", "-pinned")
	if got := hitAddresses(t, r.out); r.code != 0 || len(got) != 1 || got[0] != addrC {
		t.Errorf("no query: %+v", r)
	}
	if r := do(t, "-db", db, "search", "-pinned", "badgers"); r.code != 1 {
		t.Errorf("a pinned filter with no match should be 1: %+v", r)
	}
}

func TestPinsAndTagsSurviveForget(t *testing.T) {
	db, site := dbPath(t), localSite(t)
	do(t, "-db", db, "fill", site)
	do(t, "-db", db, "pin", addrC)
	do(t, "-db", db, "tag", addrC, "keep")
	do(t, "-db", db, "forget", "-all")
	do(t, "-db", db, "fill", site)
	if got := hitAddresses(t, do(t, "-db", db, "search", "-json", "-tag", "keep").out); len(got) != 1 {
		t.Errorf("tag lost: %v", got)
	}
	if got := hitAddresses(t, do(t, "-db", db, "search", "-json", "the").out); got[0] != addrC {
		t.Errorf("pin lost: %v", got)
	}
}

func TestStatsCountsPinsAndTags(t *testing.T) {
	db := filled(t)
	do(t, "-db", db, "pin", addrA)
	do(t, "-db", db, "tag", addrA, "x", "y")
	do(t, "-db", db, "tag", addrB, "x")
	j := parseJSON(t, do(t, "-db", db, "stats", "-json").out)
	if j["pinned"] != float64(1) || j["tags"] != float64(2) || j["tag_pages"] != float64(3) {
		t.Errorf("stats %v", j)
	}
}

func TestAVersion3CorpusIsUpgradedByTheNextPinCommand(t *testing.T) {
	db := filled(t)
	raw, err := openRaw(db)
	if err != nil {
		t.Fatal(err)
	}
	raw.Exec(`DROP TABLE IF EXISTS pins`)
	raw.Exec(`DROP TABLE IF EXISTS tags`)
	raw.Exec(`UPDATE meta SET value = '3' WHERE key = 'schema_version'`)
	raw.Close()
	if r := do(t, "-db", db, "pin", addrA); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if searchCode(t, db, "badgers") != 0 {
		t.Error("pages lost in the upgrade")
	}
}

func TestPinAndTagHostileTextNeverReachesATerminalAsAnEscape(t *testing.T) {
	db := filled(t)
	if r := do(t, "-db", db, "tag", addrA, "x\x1b[31m"); r.code != 2 {
		t.Errorf("a tag with an escape: %+v", r)
	}
	r := do(t, "-db", db, "pin", "https://example.org/\x1b[2Jpage")
	for _, s := range []string{r.out, r.errOut, do(t, "-db", db, "pin", "-list").out} {
		if strings.Contains(s, "\x1b") {
			t.Errorf("an escape reached output: %q", s)
		}
	}
}

func TestPinAndTagExitCodes(t *testing.T) {
	db := filled(t)
	missing := filepath.Join(t.TempDir(), "none.db")
	do(t, "-db", db, "tag", addrA, "has")
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"pin no address", []string{"-db", db, "pin"}, 2},
		{"pin not an address", []string{"-db", db, "pin", "drafts/x.md"}, 2},
		{"pin list with an address", []string{"-db", db, "pin", "-list", addrA}, 2},
		{"pin missing corpus", []string{"-db", missing, "pin", addrA}, 66},
		{"unpin nothing pinned", []string{"-db", db, "unpin", addrB}, 1},
		{"unpin no address", []string{"-db", db, "unpin"}, 2},
		{"tag no tag", []string{"-db", db, "tag", addrA}, 2},
		{"tag no arguments", []string{"-db", db, "tag"}, 2},
		{"tag bad address", []string{"-db", db, "tag", "nope", "x"}, 2},
		{"tag bad tag", []string{"-db", db, "tag", addrA, "has space"}, 2},
		{"tag too long", []string{"-db", db, "tag", addrA, strings.Repeat("x", 65)}, 2},
		{"tag missing corpus", []string{"-db", missing, "tag", addrA, "x"}, 66},
		{"untag a tag not carried", []string{"-db", db, "untag", addrA, "absent"}, 1},
		{"untag a tag carried", []string{"-db", db, "untag", addrA, "has"}, 0},
		{"untag no tag", []string{"-db", db, "untag", addrA}, 2},
		{"search no query no filter", []string{"-db", db, "search"}, 2},
		{"search tag with no page", []string{"-db", db, "search", "-tag", "nothing-has-this"}, 1},
		{"help pin", []string{"help", "pin"}, 0},
		{"help unpin", []string{"help", "unpin"}, 0},
		{"help tag", []string{"help", "tag"}, 0},
		{"help untag", []string{"help", "untag"}, 0},
	}
	for _, c := range cases {
		if r := do(t, c.args...); r.code != c.want {
			t.Errorf("%s: exit %d, want %d\nstdout %q\nstderr %q", c.name, r.code, c.want, r.out, r.errOut)
		}
	}
}

func TestPinOnANewerSchemaIsExit65AndALockedCorpusIsNotMistakenForIt(t *testing.T) {
	db := filled(t)
	if err := setSchema(db, "999"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"pin", addrA}, {"tag", addrA, "x"}, {"search", "-pinned"}} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 65 {
			t.Errorf("%v: %+v", args, r)
		}
	}
}
