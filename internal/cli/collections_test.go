package cli

// Phase 5a tests: fill -name NAME FOLDER reads a folder of Markdown files with
// no outline as a named collection, stored under collection://NAME/path.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files (slash-separated names) under a new folder.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var writingFiles = map[string]string{
	"top.md":               "# Top note\n\nWombats sleep all day.\n",
	"essays/a b.md":        "# Essay A\n\nQuokkas smile.\n",
	"essays/deep/c.md":     "---\ntitle: Deep Title\n---\nNumbats eat termites.\n",
	"essays/café.markdown": "# Café\n\nPlatypuses lay eggs.\n",
	"plain.md":             "just words without a heading, about kiwis\n",
}

func collDB(t *testing.T) (db, root string) {
	t.Helper()
	t.Setenv("CMARKFIND_INDEX", "")
	return dbPath(t), writeTree(t, writingFiles)
}

func TestAFolderWithNoOutlineFilledByNameIsSearchableUnderCollectionAddresses(t *testing.T) {
	db, root := collDB(t)
	r := do(t, "-db", db, "fill", "-name", "sandbox-writing", root)
	if r.code != 0 || !strings.Contains(r.out, "added 5") {
		t.Fatalf("fill: %+v", r)
	}
	for q, want := range map[string]string{
		"quokkas":    "collection://sandbox-writing/essays/a%20b.md",
		"numbats":    "collection://sandbox-writing/essays/deep/c.md",
		"platypuses": "collection://sandbox-writing/essays/caf%C3%A9.markdown",
		"wombats":    "collection://sandbox-writing/top.md",
		"kiwis":      "collection://sandbox-writing/plain.md",
	} {
		if got := setHits(t, db, "search", q); len(got) != 1 || got[0] != want {
			t.Errorf("%s: %v, want %s", q, got, want)
		}
	}
	titles := map[string]string{}
	for _, h := range parseJSON(t, do(t, "-db", db, "search", "-json", "-limit", "20", "the OR a OR kiwis OR numbats OR wombats OR quokkas OR platypuses").out)["hits"].([]any) {
		m := h.(map[string]any)
		titles[m["address"].(string)] = m["title"].(string)
	}
	for addr, want := range map[string]string{
		"collection://sandbox-writing/top.md":           "Top note",
		"collection://sandbox-writing/essays/deep/c.md": "Deep Title", // front matter beats a heading
		"collection://sandbox-writing/plain.md":         "plain",      // no title, no heading: the file name
	} {
		if titles[addr] != want {
			t.Errorf("title of %s = %q, want %q", addr, titles[addr], want)
		}
	}
}

func TestACollectionIsASetNamedByItsName(t *testing.T) {
	db, root := collDB(t)
	do(t, "-db", db, "fill", "-name", "sandbox-writing", root)
	if got := setNames(t, db); len(got) != 1 || got[0] != "sandbox-writing" {
		t.Fatalf("sets %v", got)
	}
	j := parseJSON(t, do(t, "-db", db, "set", "show", "-json", "sandbox-writing").out)
	if j["pages"] != float64(5) || j["root"] != root {
		t.Errorf("set show: %v", j)
	}
	if got := setHits(t, db, "search-set", "sandbox-writing", "wombats"); len(got) != 1 {
		t.Errorf("search-set: %v", got)
	}
	if r := do(t, "-db", db, "set", "ignore", "sandbox-writing"); r.code != 0 {
		t.Errorf("ignore: %+v", r)
	}
	if r := do(t, "-db", db, "search", "wombats"); r.code != 1 {
		t.Errorf("an ignored collection is searched: %+v", r)
	}
}

func TestRefillingACollectionChangesWhatChangedAndMarksTheRestGone(t *testing.T) {
	db, root := collDB(t)
	do(t, "-db", db, "fill", "-name", "w", root)
	r := do(t, "-db", db, "fill", "-name", "w", root)
	if r.code != 0 || !strings.Contains(r.out, "unchanged 5") || !strings.Contains(r.out, "added 0") {
		t.Fatalf("second fill: %+v", r)
	}
	os.WriteFile(filepath.Join(root, "top.md"), []byte("# Top note\n\nWombats sleep all day and quolls hunt at night.\n"), 0o644)
	if r := do(t, "-db", db, "fill", "-name", "w", root); !strings.Contains(r.out, "updated 1") {
		t.Errorf("edit: %+v", r)
	}
	if r := do(t, "-db", db, "search", "quolls"); r.code != 0 {
		t.Errorf("the edit is not searchable: %+v", r)
	}
	os.Rename(filepath.Join(root, "plain.md"), filepath.Join(root, "renamed.md"))
	r = do(t, "-db", db, "fill", "-name", "w", root)
	if !strings.Contains(r.out, "added 1") || !strings.Contains(r.out, "gone 1") {
		t.Errorf("rename: %+v", r)
	}
	os.Remove(filepath.Join(root, "essays", "a b.md"))
	if r := do(t, "-db", db, "fill", "-name", "w", root); !strings.Contains(r.out, "gone 1") {
		t.Errorf("delete: %+v", r)
	}
	if r := do(t, "-db", db, "search", "quokkas"); r.code != 1 {
		t.Errorf("a deleted file is still found: %+v", r)
	}
}

func TestTheWalkSkipsDotFoldersLinksOtherFilesAndAFileOverTheLimit(t *testing.T) {
	db := dbPath(t)
	t.Setenv("CMARKFIND_INDEX", "")
	outside := writeTree(t, map[string]string{"out.md": "# Out\n\noutsider words\n", "d/inner.md": "# Inner\n\ninsider words\n"})
	root := writeTree(t, map[string]string{
		"keep.md":            "# Keep\n\nkept words\n",
		".git/config.md":     "# Git\n\ngit words\n",
		".hidden.md":         "# Hidden\n\nhidden words\n",
		"notes.txt":          "text words\n",
		"sub/.obsidian/x.md": "# Obs\n\nobsidian words\n",
		"UPPER.MD":           "# Upper\n\nupper words\n",
	})
	os.WriteFile(filepath.Join(root, "huge.md"), []byte("# Huge\n\n"+strings.Repeat("word ", 9*1024*1024/5)), 0o644)
	if err := os.Symlink(filepath.Join(outside, "out.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skip("no symbolic links here")
	}
	os.Symlink(filepath.Join(outside, "d"), filepath.Join(root, "linkdir"))
	r := do(t, "-db", db, "fill", "-name", "w", root)
	if r.code != 0 || !strings.Contains(r.out, "added 2") || !strings.Contains(r.out, "skipped 1") {
		t.Fatalf("fill: %+v", r)
	}
	for _, w := range []string{"git", "hidden", "text", "obsidian", "outsider", "insider"} {
		if r := do(t, "-db", db, "search", w+" words"); r.code != 1 {
			t.Errorf("%q was walked: %+v", w, r)
		}
	}
	for _, w := range []string{"kept", "upper"} {
		if r := do(t, "-db", db, "search", w+" words"); r.code != 0 {
			t.Errorf("%q was not found: %+v", w, r)
		}
	}
	if !strings.Contains(r.errOut, "huge.md") || !strings.Contains(r.errOut, "symbolic link") {
		t.Errorf("the warnings: %q", r.errOut)
	}
}

func TestAnUnreadableFileIsCountedTheOthersAreStoredAndTheExitIs77(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	db, root := collDB(t)
	os.Chmod(filepath.Join(root, "top.md"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "top.md"), 0o644) })
	r := do(t, "-db", db, "fill", "-name", "w", root)
	if r.code != 77 || !strings.Contains(r.out, "added 4") || !strings.Contains(r.out, "failed 1") {
		t.Errorf("%+v", r)
	}
}

func TestCollectionNamesAndOptionsThatDoNotGoTogetherAreExit2(t *testing.T) {
	db, root := collDB(t)
	for _, name := range []string{"Notes", "a.b", "", "-x", "a b", "a/b", "x:y", strings.Repeat("a", 41), "http"} {
		if name == "http" {
			continue // a valid name
		}
		if r := do(t, "-db", db, "fill", "-name", name, root); r.code != 2 {
			t.Errorf("name %q: %+v", name, r)
		}
	}
	for _, args := range [][]string{
		{"fill", "-name", "w", "-site-url", "https://example.org/", root},
		{"fill", "-name", "w", "https://example.org/"},
		{"fill", "-name", "w", "-depth", "2", root},
		{"fill", "-name", "w", "-batch", "5", root},
	} {
		if r := do(t, append([]string{"-db", db}, args...)...); r.code != 2 {
			t.Errorf("%v: %+v", args, r)
		}
	}
	if r := do(t, "-db", db, "fill", "-name", "w", filepath.Join(t.TempDir(), "missing")); r.code != 66 {
		t.Errorf("a missing folder: %+v", r)
	}
	if r := do(t, "-db", db, "fill", "-name", "w", filepath.Join(root, "top.md")); r.code != 2 {
		t.Errorf("a file is not a folder: %+v", r)
	}
}

func TestOneFolderUnderTwoNamesIsTwoSetsAndAnotherFolderUnderATakenNameIsRefused(t *testing.T) {
	db, root := collDB(t)
	do(t, "-db", db, "fill", "-name", "one", root)
	if r := do(t, "-db", db, "fill", "-name", "two", root); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := setNames(t, db); len(got) != 2 {
		t.Errorf("sets %v", got)
	}
	other := writeTree(t, map[string]string{"x.md": "# X\n\nother\n"})
	r := do(t, "-db", db, "fill", "-name", "one", other)
	if r.code != 2 || !strings.Contains(r.errOut, root) {
		t.Errorf("another folder under a taken name: %+v", r)
	}
}

func TestForgetByCollectionNameRemovesPagesRowAndSet(t *testing.T) {
	db, root := collDB(t)
	do(t, "-db", db, "fill", "-name", "w", root)
	r := do(t, "-db", db, "forget", "-dry-run", "w")
	if r.code != 0 || !strings.Contains(r.out, "would forget 5") {
		t.Errorf("dry run: %+v", r)
	}
	r = do(t, "-db", db, "forget", "w")
	if r.code != 0 || !strings.Contains(r.out, "forgot 5") {
		t.Fatalf("forget: %+v", r)
	}
	if got := setNames(t, db); len(got) != 0 {
		t.Errorf("sets %v", got)
	}
	// the folder can now go under another name
	if r := do(t, "-db", db, "fill", "-name", "w", writeTree(t, map[string]string{"y.md": "# Y\n\nfresh\n"})); r.code != 0 {
		t.Errorf("the name is free again: %+v", r)
	}
}

func TestAnOutlineInTheFolderIsIgnoredWithNameAndReadWithout(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	site := localSite(t) // a.md, b.md, c.md and an outline of them
	os.WriteFile(filepath.Join(site, "extra.md"), []byte("# Extra\n\nnot in the outline\n"), 0o644)
	db := dbPath(t)
	if r := do(t, "-db", db, "fill", "-name", "folder", site); r.code != 0 || !strings.Contains(r.out, "added 4") {
		t.Errorf("as a collection every file: %+v", r)
	}
	db2 := dbPath(t)
	if r := do(t, "-db", db2, "fill", site); r.code != 0 || !strings.Contains(r.out, "added 3") {
		t.Errorf("as a site only the outline's: %+v", r)
	}
	// the same folder as a site and as a collection: two sets
	if r := do(t, "-db", db, "fill", site); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := setNames(t, db); len(got) != 2 {
		t.Errorf("sets %v", got)
	}
}

func TestACollectionIsExcludedByItsPrefixAndLimitedByMaxPages(t *testing.T) {
	db, root := collDB(t)
	do(t, "-db", db, "fill", "-name", "w", root)
	if r := do(t, "-db", db, "exclude", "collection://w/essays/"); r.code != 0 {
		t.Fatalf("exclude: %+v", r)
	}
	r := do(t, "-db", db, "fill", "-name", "w", root)
	if r.code != 0 || !strings.Contains(r.out, "unchanged 2") || !strings.Contains(r.out, "excluded 3") {
		t.Errorf("fill with an exclusion: %+v", r)
	}
	if r := do(t, "-db", db, "search", "quokkas"); r.code != 1 {
		t.Errorf("excluded: %+v", r)
	}
	db2 := dbPath(t)
	r = do(t, "-db", db2, "fill", "-name", "w", "-max-pages", "2", root)
	if r.code != 0 || !strings.Contains(r.out, "added 2") {
		t.Errorf("-max-pages: %+v", r)
	}
	// a fill cut short marks nothing gone
	if r := do(t, "-db", db2, "fill", "-name", "w", "-max-pages", "1", root); strings.Contains(r.out, "gone 1") {
		t.Errorf("a cut fill marked pages gone: %+v", r)
	}
}

func TestAFolderWithNoMarkdownExitsAsAnEmptyOutlineDoes(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	empty := writeTree(t, map[string]string{"notes.txt": "x"})
	r := do(t, "-db", dbPath(t), "fill", "-name", "w", empty)
	emptyOutline := t.TempDir()
	os.MkdirAll(filepath.Join(emptyOutline, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(emptyOutline, ".well-known", "toc.opml"), []byte(`<opml version="2.0"><head/><body></body></opml>`), 0o644)
	o := do(t, "-db", dbPath(t), "fill", emptyOutline)
	if r.code != o.code {
		t.Errorf("collection %+v, outline %+v", r, o)
	}
}

// A note whose front matter is not valid YAML fails as a web page does: it is
// counted and named, the other files are stored, and the exit is 65 (data).
func TestANoteWithBrokenFrontMatterIsCountedNamedAndTheRestStored(t *testing.T) {
	db := dbPath(t)
	t.Setenv("CMARKFIND_INDEX", "")
	root := writeTree(t, map[string]string{
		"bad.md":  "---\ntitle: [unclosed\n---\nechidnas curl up\n",
		"good.md": "# Good\n\nkoalas doze\n",
	})
	r := do(t, "-db", db, "fill", "-name", "w", root)
	if r.code != 65 || !strings.Contains(r.out, "added 1") || !strings.Contains(r.out, "failed 1") || !strings.Contains(r.errOut, "bad.md") {
		t.Fatalf("%+v", r)
	}
	if q := do(t, "-db", db, "search", "koalas"); q.code != 0 {
		t.Errorf("the good file is not stored: %+v", q)
	}
}

func TestTheFillTopicDocumentsName(t *testing.T) {
	r := do(t, "help", "fill")
	if r.code != 0 || !strings.Contains(r.out, "-name") || !strings.Contains(r.out, "collection://") {
		t.Errorf("fill topic: %.200s", r.out)
	}
	if j, _ := verbFlags("fill"); !strings.Contains(flagNames(j), "name") {
		t.Errorf("fill flags %v", j)
	}
}
