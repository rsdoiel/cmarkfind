package corpus

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Tests for the document-only words as the corpus sees them: four pages with
// 600,000 "the" each (a page over 8 MiB is refused by the fill, and a file over half the limit is warned about) are over
// the entry budget (2 MiB), so no test knob is needed.

func bigPage() *string {
	return s("# Big\n\n" + strings.Repeat("the ", 600_000) + "\n\nzebra stripes\n")
}

func TestAFillOfAVeryCommonWordIndexesItDocumentOnlyAndSaysSo(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	root := site(t, map[string]*string{"big1.md": bigPage(), "big2.md": bigPage(), "big3.md": bigPage(), "big4.md": bigPage(), "small.md": s("# Small\n\nThe zebra and the otter.\n")})
	fill(t, db, root)
	w := db.IndexWarnings()
	if len(w) != 1 || !strings.Contains(w[0], "without positions") || !strings.Contains(w[0], "the") {
		t.Fatalf("warnings %q", w)
	}
	if again := db.IndexWarnings(); len(again) != 0 {
		t.Errorf("warnings are read once: %q", again)
	}
	st, err := db.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.DocumentOnly, []string{"the"}) {
		t.Errorf("stats document-only %v", st.DocumentOnly)
	}
	if r := search(t, db, "the"); len(r.Hits) != 5 {
		t.Errorf("search the: %v", addresses(r))
	}
	if r := search(t, db, "the zebra"); len(r.Hits) != 5 {
		t.Errorf("search the zebra: %v", addresses(r))
	}

	// another page arrives: the index is rebuilt, the same word is still document-only, nothing is said
	os.WriteFile(filepath.Join(root, "third.md"), []byte("# Third\n\nAn otter.\n"), 0o644)
	writeOutline(t, root, `<outline text="b1" url="https://example.org/big1.md"/><outline text="b2" url="https://example.org/big2.md"/><outline text="b3" url="https://example.org/big3.md"/><outline text="b4" url="https://example.org/big4.md"/><outline text="s" url="https://example.org/small.md"/><outline text="t" url="https://example.org/third.md"/>`)
	if rep := fill(t, db, root); rep.Added != 1 {
		t.Fatalf("fill %+v", rep)
	}
	if w := db.IndexWarnings(); len(w) != 0 {
		t.Errorf("the set of words did not change but %q", w)
	}

	// the big pages go: no document-only word is left, and that is not worth a warning
	writeOutline(t, root, `<outline text="s" url="https://example.org/small.md"/><outline text="t" url="https://example.org/third.md"/>`)
	fill(t, db, root)
	if w := db.IndexWarnings(); len(w) != 0 {
		t.Errorf("warnings after the word stopped being document-only: %q", w)
	}
	if st, _ := db.Stats(context.Background()); len(st.DocumentOnly) != 0 {
		t.Errorf("stats document-only %v", st.DocumentOnly)
	}
}

func TestACorpusWithNoVeryCommonWordIsSilent(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	if w := db.IndexWarnings(); len(w) != 0 {
		t.Errorf("warnings %q", w)
	}
	if st, _ := db.Stats(context.Background()); len(st.DocumentOnly) != 0 {
		t.Errorf("stats document-only %v", st.DocumentOnly)
	}
}

func TestAPhraseThroughADocumentOnlyWordFindsOnlyThePagesThatHoldIt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2.4 MB entry")
	}
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	fill(t, db, site(t, map[string]*string{
		"big1.md": bigPage(), "big2.md": bigPage(), "big3.md": bigPage(), "big4.md": bigPage(),
		"small.md": s("# Small\n\nThe zebra and the otter.\n"),
		"apart.md": s("# Apart\n\nA zebra stripes, and then the otter.\n"), // both words, not "the zebra"
	}))
	if st, _ := db.Stats(context.Background()); !slices.Equal(st.DocumentOnly, []string{"the"}) {
		t.Fatalf("document-only %v", st.DocumentOnly)
	}
	got := addresses(search(t, db, `"the zebra"`))
	if len(got) != 5 || slices.Contains(got, "https://example.org/apart.md") {
		t.Errorf(`"the zebra": %v`, got)
	}
	if got := addresses(search(t, db, `-"the zebra"`)); !slices.Equal(got, []string{"https://example.org/apart.md"}) {
		t.Errorf(`-"the zebra": %v`, got)
	}
	if got := addresses(search(t, db, `otter -"the zebra"`)); !slices.Equal(got, []string{"https://example.org/apart.md"}) {
		t.Errorf(`otter -"the zebra": %v`, got)
	}
}
