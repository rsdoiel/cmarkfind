package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// indexNames reads the topic names from `help index`.
func indexNames(t *testing.T) []string {
	t.Helper()
	r := do(t, "help", "index")
	if r.code != 0 {
		t.Fatalf("help index: exit %d, stderr %q", r.code, r.errOut)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(r.out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			names = append(names, f[0])
		}
	}
	return names
}

func TestHelpIndexListsEveryVerbAndTheConceptTopics(t *testing.T) {
	got := strings.Join(indexNames(t), " ")
	for _, want := range []string{"fill", "search", "stats", "forget", "exclude", "unexclude", "query", "corpus", "exit-status"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Errorf("help index lacks %q: %s", want, got)
		}
	}
}

func TestHelpIndexJSON(t *testing.T) {
	r := do(t, "help", "index", "-json")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.errOut)
	}
	var v struct {
		Topics []struct{ Name, Summary string }
	}
	if err := json.Unmarshal([]byte(r.out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.out)
	}
	if len(v.Topics) != len(indexNames(t)) {
		t.Errorf("JSON has %d topics, plain text has %d", len(v.Topics), len(indexNames(t)))
	}
	for _, x := range v.Topics {
		if x.Name == "" || x.Summary == "" {
			t.Errorf("topic %+v has no name or summary", x)
		}
	}
}

// Every topic is a man page of section 7 that pandoc can render: front matter,
// then NAME and DESCRIPTION.
func TestEveryTopicIsASectionSevenPage(t *testing.T) {
	for _, name := range indexNames(t) {
		r := do(t, "help", name)
		if r.code != 0 {
			t.Errorf("help %s: exit %d, stderr %q", name, r.code, r.errOut)
			continue
		}
		if !strings.HasPrefix(r.out, "---\ntitle: cmarkfind-"+name+"\nsection: 7\n") {
			t.Errorf("help %s does not start with section 7 front matter:\n%.120s", name, r.out)
		}
		for _, h := range []string{"# NAME", "# DESCRIPTION"} {
			if !strings.Contains(r.out, "\n"+h+"\n") {
				t.Errorf("help %s has no %q heading", name, h)
			}
		}
		if strings.Contains(r.out, "{app_name}") || strings.Contains(r.out, "{version}") {
			t.Errorf("help %s leaves a placeholder unfilled", name)
		}
	}
}

func TestHelpIndexAndTheManPageAreNamedInTheMainPage(t *testing.T) {
	if !strings.Contains(HelpText, "help index") {
		t.Error("the cmarkfind(1) page does not mention `help index`")
	}
	for _, name := range indexNames(t) {
		if !strings.Contains(HelpText, "cmarkfind-"+name+"(7)") {
			t.Errorf("SEE ALSO of the cmarkfind(1) page lacks cmarkfind-%s(7)", name)
		}
	}
}

// body drops the front matter, which carries the version and the commit.
func body(s string) string {
	if strings.HasPrefix(s, "---\n") {
		if _, rest, ok := strings.Cut(s[4:], "\n---\n"); ok {
			return rest
		}
	}
	return s
}

// The committed pages are what `help TOPIC` says now: `make build` writes them.
func TestTopicPagesAreCurrent(t *testing.T) {
	names := indexNames(t)
	for _, name := range names {
		file := filepath.Join("..", "..", "cmarkfind-"+name+".7.md")
		have, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: %v (run make build)", file, err)
			continue
		}
		if want := body(do(t, "help", name).out); body(string(have)) != want {
			t.Errorf("%s is out of date: run make build", file)
		}
	}
	pages, _ := filepath.Glob(filepath.Join("..", "..", "*.7.md"))
	var onDisk, listed []string
	for _, p := range pages {
		onDisk = append(onDisk, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "cmarkfind-"), ".7.md"))
	}
	listed = append(listed, names...)
	sort.Strings(onDisk)
	sort.Strings(listed)
	if strings.Join(onDisk, " ") != strings.Join(listed, " ") {
		t.Errorf("*.7.md pages %v, help index %v", onDisk, listed)
	}
}

// The Makefile's TOPICS line is what `make build` loops over.
func TestMakefileTopicsAreTheIndex(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^TOPICS *= *(.*)$`).FindSubmatch(mk)
	if m == nil {
		t.Fatal("Makefile has no TOPICS line")
	}
	got := strings.Fields(string(m[1]))
	want := indexNames(t)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Makefile TOPICS = %v, help index = %v", got, want)
	}
}

func TestUnknownTopicIsUsageAndNamesTheIndex(t *testing.T) {
	r := do(t, "help", "nope")
	if r.code != 2 || !strings.Contains(r.errOut, "help index") {
		t.Errorf("exit %d, stderr %q: want 2 and a pointer to `help index`", r.code, r.errOut)
	}
}
