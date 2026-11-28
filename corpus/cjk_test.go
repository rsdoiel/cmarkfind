package corpus

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rsdoiel/cmarkfind/index"
)

var cjkPages = map[string]*string{
	"neko.md":  s("---\ntitle: 吾輩は猫である\n---\n吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。\n"),
	"tokyo.md": s("# Tokyo\n\n東京都に行きます。日本語のテキストです。\n"),
	"latin.md": s("# Cats\n\nThe cat sat on the mat.\n"),
}

func TestAChineseOrJapanesePageIsFoundByACharacterOrAPhrase(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, cjkPages))
	for q, want := range map[string]string{
		"猫":     "https://example.org/neko.md",
		"名前はまだ": "https://example.org/neko.md",
		"東京都":   "https://example.org/tokyo.md",
		"日本語":   "https://example.org/tokyo.md",
	} {
		got := addresses(search(t, db, q))
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: %v, want %s", q, got, want)
		}
	}
	r := search(t, db, "猫")
	if len(r.Hits) != 1 || !strings.Contains(r.Hits[0].Snippet, "吾輩は") {
		t.Errorf("snippet %+v", r.Hits)
	}
}

func TestAnIndexOfTheEarlierWordRuleIsRebuiltAndFindsChineseAndJapanese(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, cjkPages))
	mp := filepath.Join(db.IndexDir(), "manifest.json")
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	old := regexp.MustCompile(`"analyzer": "[^"]*"`).ReplaceAllString(string(raw), `"analyzer": "unicode-word-v2"`)
	if err := os.WriteFile(mp, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Open(db.IndexDir()); err == nil {
		t.Fatal("a v2 index opened")
	}
	res, err := db.Search(context.Background(), "猫", 10)
	if err != nil || len(res.Hits) != 1 {
		t.Fatalf("search over a v2 index: %+v, %v", res, err)
	}
}
