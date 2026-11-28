package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerbsListsEveryVerbWithItsSummary(t *testing.T) {
	r := do(t, "verbs")
	if r.code != 0 || r.errOut != "" {
		t.Fatalf("%+v", r)
	}
	lines := strings.Split(strings.TrimRight(r.out, "\n"), "\n")
	if len(lines) != len(verbSpecs()) {
		t.Fatalf("%d lines for %d verbs:\n%s", len(lines), len(verbSpecs()), r.out)
	}
	for i, v := range verbSpecs() {
		f := strings.Fields(lines[i])
		if len(f) < 2 || f[0] != v.Name || !strings.Contains(lines[i], v.Summary) {
			t.Errorf("line %d = %q, want %s and %q", i, lines[i], v.Name, v.Summary)
		}
	}
}

func TestVerbsJSONHasSubverbsAndFlags(t *testing.T) {
	r := do(t, "verbs", "-json")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var v struct {
		Verbs []struct {
			Name     string
			Summary  string
			Subverbs []string
			Flags    []struct {
				Name string
				Bool bool
			}
			Sub map[string][]struct {
				Name string
				Bool bool
			}
		}
	}
	if err := json.Unmarshal([]byte(r.out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.out)
	}
	if len(v.Verbs) != len(verbSpecs()) {
		t.Fatalf("%d verbs", len(v.Verbs))
	}
	by := map[string]int{}
	for i, x := range v.Verbs {
		by[x.Name] = i
		if x.Subverbs == nil {
			t.Errorf("%s: subverbs must be [] not null", x.Name)
		}
	}
	fill := v.Verbs[by["fill"]]
	found := false
	for _, f := range fill.Flags {
		if f.Name == "site-url" && !f.Bool {
			found = true
		}
	}
	if !found {
		t.Errorf("fill flags %+v lack -site-url", fill.Flags)
	}
	idx := v.Verbs[by["indexer"]]
	if strings.Join(idx.Subverbs, " ") != "build validate list discover" {
		t.Errorf("indexer subverbs %v", idx.Subverbs)
	}
	var chunk bool
	for _, f := range idx.Sub["build"] {
		chunk = chunk || f.Name == "chunk"
	}
	if !chunk {
		t.Errorf("indexer build flags missing -chunk: %+v", idx.Sub)
	}
}

func TestVerbsNeedsNoCorpusAndTakesNoArguments(t *testing.T) {
	db := filepath.Join(t.TempDir(), "never", "corpus.db")
	if r := do(t, "-db", db, "verbs"); r.code != 0 {
		t.Errorf("verbs with no corpus: %+v", r)
	}
	if r := do(t, "verbs", "fill"); r.code != 2 {
		t.Errorf("verbs fill: %+v", r)
	}
	if r := do(t, "verbs", "-nosuch"); r.code != 2 {
		t.Errorf("verbs -nosuch: %+v", r)
	}
}
