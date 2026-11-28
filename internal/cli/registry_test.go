package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func specNames() []string {
	var n []string
	for _, v := range verbSpecs() {
		n = append(n, v.Name)
	}
	return n
}

func TestTheRegistryNamesEveryVerb(t *testing.T) {
	want := "completion exclude fill forget help indexer pin search search-set set stats tag unexclude unpin untag verbs"
	got := specNames()
	sort.Strings(got)
	if strings.Join(got, " ") != want {
		t.Errorf("verbs\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	for _, v := range verbSpecs() {
		if v.Summary == "" {
			t.Errorf("%s has no summary", v.Name)
		}
	}
}

// Every verb in the table is dispatched, and a verb that is not in it is not.
func TestEveryRegisteredVerbIsDispatched(t *testing.T) {
	for _, name := range specNames() {
		if name == "help" || name == "completion" || name == "verbs" {
			continue
		}
		r := do(t, "-db", filepath.Join(t.TempDir(), "none.db"), name, "-nosuchflag")
		if r.code != 2 || strings.Contains(r.errOut, "unknown verb") {
			t.Errorf("%s: exit %d, stderr %q (want a flag usage error from the verb itself)", name, r.code, r.errOut)
		}
	}
	r := do(t, "frobnicate")
	if r.code != 2 || !strings.Contains(r.errOut, "unknown verb") {
		t.Fatalf("%+v", r)
	}
	for _, name := range specNames() {
		if !strings.Contains(r.errOut, name) {
			t.Errorf("the unknown-verb message does not list %s: %q", name, r.errOut)
		}
	}
	r = do(t)
	if r.code != 2 || !strings.Contains(r.errOut, "no verb") || !strings.Contains(r.errOut, "unexclude") {
		t.Errorf("no verb: %+v", r)
	}
}

// A verb has a manual page of its own, except help.
func TestEveryRegisteredVerbHasATopic(t *testing.T) {
	topics := " " + strings.Join(indexNames(t), " ") + " "
	for _, name := range specNames() {
		if name != "help" && !strings.Contains(topics, " "+name+" ") {
			t.Errorf("no help topic for verb %s", name)
		}
	}
}

func flagNames(fl []flagInfo) string {
	var n []string
	for _, f := range fl {
		n = append(n, f.Name)
	}
	sort.Strings(n)
	return strings.Join(n, " ")
}

// The flags are read from the verbs' own FlagSets, so the table cannot drift.
func TestVerbFlagsAreReadFromTheVerbs(t *testing.T) {
	cases := map[string]string{
		"fill":          "batch depth every json max-pages name pause robots site-url until-done user-agent",
		"search":        "json limit pinned tag",
		"search-set":    "json limit pinned tag",
		"set fields":    "json reset",
		"stats":         "json",
		"forget":        "all dry-run json",
		"indexer build": "chunk json",
		"pin":           "json list",
		"completion":    "check install json uninstall",
	}
	for name, want := range cases {
		fl, err := verbFlags(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := flagNames(fl); got != want {
			t.Errorf("%s flags\n got %s\nwant %s", name, got, want)
		}
	}
	fl, _ := verbFlags("fill")
	kinds := map[string]bool{}
	for _, f := range fl {
		kinds[f.Name] = f.Bool
	}
	if !kinds["robots"] || !kinds["json"] || kinds["pause"] || kinds["site-url"] {
		t.Errorf("bool and value flags confused: %v", kinds)
	}
	if _, err := verbFlags("nosuch"); err == nil {
		t.Error("an unknown verb has no flags")
	}
}

// Reading the flags runs nothing: no file is made, nothing is opened.
func TestReadingTheFlagsHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("CMARKFIND_DB", filepath.Join(dir, "corpus.db"))
	t.Setenv("CMARKFIND_INDEX", filepath.Join(dir, "index"))
	for _, v := range verbSpecs() {
		for _, name := range v.Paths() {
			if _, err := verbFlags(name); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("reading flags left %d files behind", len(ents))
	}
}

// Completion reads flags by name, so a flag may not be a switch in one verb and
// take a value in another.
func TestAFlagHasTheSameKindInEveryVerb(t *testing.T) {
	kinds := map[string]bool{}
	where := map[string]string{}
	for _, v := range verbSpecs() {
		for _, name := range v.Paths() {
			fl, err := verbFlags(name)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, f := range fl {
				if k, seen := kinds[f.Name]; seen && k != f.Bool {
					t.Errorf("-%s is a switch in one of %s and %s and takes a value in the other", f.Name, where[f.Name], name)
				}
				kinds[f.Name], where[f.Name] = f.Bool, name
			}
		}
	}
}

func TestIndexerHasItsSubverbs(t *testing.T) {
	for _, v := range verbSpecs() {
		if v.Name == "indexer" {
			if strings.Join(v.Subverbs, " ") != "build validate list discover add refresh" {
				t.Errorf("subverbs %v", v.Subverbs)
			}
			return
		}
	}
	t.Error("no indexer")
}
