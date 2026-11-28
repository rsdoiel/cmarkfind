package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFindsChineseAndJapaneseText(t *testing.T) {
	db := dbPath(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "neko.md"), []byte("---\ntitle: 吾輩は猫である\n---\n吾輩は猫である。名前はまだ無い。\n"), 0o644)
	os.WriteFile(filepath.Join(root, "latin.md"), []byte("# Cats\n\nThe cat sat on the mat.\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(
		`<opml version="2.0"><head/><body><outline text="n" url="https://example.org/neko.md"/><outline text="l" url="https://example.org/latin.md"/></body></opml>`), 0o644)
	if r := do(t, "-db", db, "fill", root); r.code != 0 {
		t.Fatalf("fill: %+v", r)
	}
	r := do(t, "-db", db, "search", "猫")
	if r.code != 0 || !strings.Contains(r.out, "neko.md") || strings.Contains(r.out, "latin.md") {
		t.Errorf("search 猫: %+v", r)
	}
	if !strings.Contains(r.out, "吾輩は**猫**で") {
		t.Errorf("the snippet should read as Japanese, matches in bold: %q", r.out)
	}
	if r := do(t, "-db", db, "search", "名前はまだ"); r.code != 0 || !strings.Contains(r.out, "neko.md") {
		t.Errorf("phrase: %+v", r)
	}
	if r := do(t, "-db", db, "search", "猫名"); r.code != 1 {
		t.Errorf("characters apart should be no match (exit 1): %+v", r)
	}
}
