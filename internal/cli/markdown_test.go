package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the Markdown search output, written before the code.

func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CMARKFIND_DB", "")
	t.Setenv("CMARKFIND_INDEX", "")
}

// ---- search output as Markdown ----

func markdownSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pages := []struct{ file, url, body string }{
		{"a(1).md", "https://example.org/a(1).md", "---\ntitle: Odd [title] with \\ slash\n---\nzebra stripes here.\n"},
		{"b c.md", "https://example.org/b%20c.md", "# Plain\n\nzebra herds here.\n"},
		{"c.md", "https://example.org/c.md", "---\ntitle: \"Esc \\e[31mred\"\n---\nzebra again.\n"},
	}
	var lines string
	for _, p := range pages {
		os.WriteFile(filepath.Join(root, p.file), []byte(p.body), 0o644)
		lines += fmt.Sprintf(`<outline text="%s" url="%s"/>`, p.file, p.url)
	}
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"),
		[]byte(`<?xml version="1.0"?><opml version="2.0"><head/><body>`+lines+`</body></opml>`), 0o644)
	return root
}

func TestSearchPrintsEachHitAsAMarkdownLink(t *testing.T) {
	clearEnv(t)
	db := filled(t)
	r := do(t, "-db", db, "search", "badgers")
	if !strings.Contains(r.out, "- [Badgers](https://example.org/b.md)\n") {
		t.Errorf("not a Markdown link:\n%s", r.out)
	}
	if !strings.Contains(r.out, "**Badgers**") || strings.Contains(r.out, "[Badgers] ") {
		t.Errorf("hit words should be bold:\n%s", r.out)
	}
}

func TestMarkdownLinkTextAndDestinationAreEscapedAndTerminalSafe(t *testing.T) {
	clearEnv(t)
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", markdownSite(t)); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	r := do(t, "-db", db, "search", "zebra")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{
		`[Odd \[title\] with \\ slash](https://example.org/a%281%29.md)`,
		`[Plain](https://example.org/b%20c.md)`,
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out, "\x1b") {
		t.Errorf("an escape reached the terminal: %q", r.out)
	}
}

func TestSearchJSONIsUnchangedByTheMarkdownOutput(t *testing.T) {
	clearEnv(t)
	db := filled(t)
	j := parseJSON(t, do(t, "-db", db, "search", "-json", "badgers").out)
	h := j["hits"].([]any)[0].(map[string]any)
	if h["address"] != "https://example.org/b.md" || !strings.Contains(fmt.Sprint(h["snippet"]), "[Badgers]") {
		t.Errorf("%v", h)
	}
}

func TestSearchIndexPrintsTheSameMarkdownLinks(t *testing.T) {
	clearEnv(t)
	db, _, _ := built(t)
	r := do(t, "-db", db, "search", "badgers")
	if !strings.Contains(r.out, "- [Badgers](https://example.org/b.md)\n") {
		t.Errorf("%s", r.out)
	}
}

func TestPinnedAndTaggedHitsSaySoOnTheirDetailLine(t *testing.T) {
	clearEnv(t)
	db := filled(t)
	do(t, "-db", db, "pin", "https://example.org/b.md")
	do(t, "-db", db, "tag", "https://example.org/b.md", "setts")
	r := do(t, "-db", db, "search", "badgers")
	if !strings.Contains(r.out, "  own, pinned, tags: setts\n") {
		t.Errorf("%s", r.out)
	}
}

func TestSearchLinksTheHeadingTheMatchIsUnder(t *testing.T) {
	clearEnv(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "w.md"), []byte("# Waterbirds\n\nIntro.\n\n## Shallow water\n\nThe crane wades here.\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"),
		[]byte(`<?xml version="1.0"?><opml version="2.0"><head/><body><outline text="w" url="https://example.org/w.md"/></body></opml>`), 0o644)
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", root); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	r := do(t, "-db", db, "search", "crane")
	if !strings.Contains(r.out, "(https://example.org/w.md#shallow-water)") {
		t.Errorf("no fragment:\n%s", r.out)
	}
	j := parseJSON(t, do(t, "-db", db, "search", "-json", "crane").out)
	h := j["hits"].([]any)[0].(map[string]any)
	if h["anchor"] != "shallow-water" || h["address"] != "https://example.org/w.md" {
		t.Errorf("%v", h)
	}
}
