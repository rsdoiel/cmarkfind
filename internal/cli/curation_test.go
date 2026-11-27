package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func searchCode(t *testing.T, db, q string) int { return do(t, "-db", db, "search", q).code }

func TestForgetBySiteLeavesTheOtherSiteAndKeepsWhatWasFilledByTheSameName(t *testing.T) {
	db := dbPath(t)
	one, srv := localSite(t), webSite(t)
	do(t, "-db", db, "fill", one)
	do(t, "-db", db, "fill", "-pause", "0s", srv.URL)
	r := do(t, "-db", db, "forget", one)
	if r.code != 0 || !strings.Contains(r.out, "forgot 3") {
		t.Fatalf("%+v", r)
	}
	if searchCode(t, db, "otter") != 1 {
		t.Error("forgotten pages are still found")
	}
	if searchCode(t, db, "badgers") != 0 {
		t.Error("the other site was touched")
	}
	// the address form: the root and the outline name the same site
	r = do(t, "-db", db, "forget", srv.URL)
	if r.code != 0 || !strings.Contains(r.out, "forgot 3") {
		t.Errorf("by root address: %+v", r)
	}
}

func TestForgetDryRunDeletesNothingAndAllEmptiesTheCorpus(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "forget", "-dry-run", "-all")
	if r.code != 0 || !strings.Contains(r.out, "would forget 3") {
		t.Fatalf("%+v", r)
	}
	if searchCode(t, db, "badgers") != 0 {
		t.Fatal("dry run deleted")
	}
	r = do(t, "-db", db, "forget", "-all", "-json")
	if j := parseJSON(t, r.out); r.code != 0 || j["forgotten"] != float64(3) {
		t.Errorf("%+v", r)
	}
	if st := parseJSON(t, do(t, "-db", db, "stats", "-json").out); st["pages"] != float64(0) {
		t.Errorf("stats %v", st)
	}
}

func TestExcludeRemovesStoredPagesAndTheNextFillSkipsThem(t *testing.T) {
	db, site := dbPath(t), localSite(t)
	do(t, "-db", db, "fill", site)
	r := do(t, "-db", db, "exclude", "https://example.org/b")
	if r.code != 0 || !strings.Contains(r.out, "removed 1") {
		t.Fatalf("%+v", r)
	}
	if searchCode(t, db, "badgers") != 1 {
		t.Error("an excluded page is still found")
	}
	r = do(t, "-db", db, "fill", site)
	if r.code != 0 || !strings.Contains(r.out, "excluded 1") {
		t.Errorf("fill: %+v", r)
	}
	if searchCode(t, db, "badgers") != 1 {
		t.Error("the fill brought it back")
	}
	if j := parseJSON(t, do(t, "-db", db, "stats", "-json").out); j["excluded"] != float64(1) {
		t.Errorf("stats %v", j)
	}
	if r := do(t, "-db", db, "exclude", "-list"); r.code != 0 || !strings.Contains(r.out, "https://example.org/b") {
		t.Errorf("list: %+v", r)
	}
	if r := do(t, "-db", db, "unexclude", "https://example.org/b"); r.code != 0 {
		t.Errorf("unexclude: %+v", r)
	}
	do(t, "-db", db, "fill", site)
	if searchCode(t, db, "badgers") != 0 {
		t.Error("after unexclude a fill should bring it back")
	}
}

func TestExcludeListOfNothingIsExitZeroAndValidJSON(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "exclude", "-list", "-json")
	if j := parseJSON(t, r.out); r.code != 0 || len(j["excluded"].([]any)) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestCurationExitCodes(t *testing.T) {
	db := filled(t)
	missing := filepath.Join(t.TempDir(), "none.db")
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"forget nothing named", []string{"-db", db, "forget"}, 2},
		{"forget a site and -all", []string{"-db", db, "forget", "-all", "x"}, 2},
		{"forget two sites", []string{"-db", db, "forget", "a", "b"}, 2},
		{"forget a site that stored nothing", []string{"-db", db, "forget", t.TempDir()}, 1},
		{"forget missing corpus", []string{"-db", missing, "forget", "-all"}, 66},
		{"exclude no prefix", []string{"-db", db, "exclude"}, 2},
		{"exclude not an address", []string{"-db", db, "exclude", "drafts/"}, 2},
		{"exclude list with a prefix", []string{"-db", db, "exclude", "-list", "https://x.org/"}, 2},
		{"exclude missing corpus", []string{"-db", missing, "exclude", "https://x.org/"}, 66},
		{"unexclude no rule", []string{"-db", db, "unexclude", "https://x.org/"}, 1},
		{"unexclude no prefix", []string{"-db", db, "unexclude"}, 2},
		{"help forget", []string{"help", "forget"}, 0},
		{"help exclude", []string{"help", "exclude"}, 0},
		{"help unexclude", []string{"help", "unexclude"}, 0},
	}
	for _, c := range cases {
		if r := do(t, c.args...); r.code != c.want {
			t.Errorf("%s: exit %d, want %d\nstdout %q\nstderr %q", c.name, r.code, c.want, r.out, r.errOut)
		}
	}
}

func TestExcludeOnANewerSchemaIsExit65(t *testing.T) {
	db := filled(t)
	if err := setSchema(db, "999"); err != nil {
		t.Fatal(err)
	}
	if r := do(t, "-db", db, "exclude", "https://x.org/"); r.code != 65 {
		t.Errorf("%+v", r)
	}
}

func TestAVersion1CorpusIsUpgradedByTheNextCommand(t *testing.T) {
	db := filled(t)
	raw, err := openRaw(db)
	if err != nil {
		t.Fatal(err)
	}
	raw.Exec(`DROP TABLE excluded`)
	raw.Exec(`UPDATE meta SET value = '1' WHERE key = 'schema_version'`)
	raw.Close()
	if r := do(t, "-db", db, "exclude", "https://example.org/c.md"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(db); err != nil || searchCode(t, db, "badgers") != 0 {
		t.Error("pages lost")
	}
}
