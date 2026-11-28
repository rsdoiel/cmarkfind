package corpus

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsdoiel/markdownweb/toc"
)

func TestACollectionAddressHasNoDriveNoRootAndEncodesEachSegment(t *testing.T) {
	for rel, want := range map[string]string{
		"a.md":           "collection://notes/a.md",
		"essays/a b.md":  "collection://notes/essays/a%20b.md",
		"x/y/z.markdown": "collection://notes/x/y/z.markdown",
		"café/menu.md":   "collection://notes/caf%C3%A9/menu.md",
		"odd#name?.md":   "collection://notes/odd%23name%3F.md",
		"back\\slash.md": "collection://notes/back%5Cslash.md", // on a system where \ is not a separator
	} {
		if got := collectionAddress("notes", rel); got != want {
			t.Errorf("%q: %q, want %q", rel, got, want)
		}
		if u, err := url.Parse(collectionAddress("notes", rel)); err != nil || u.Host != "notes" {
			t.Errorf("%q does not parse as a URL with the name as its host: %v", rel, err)
		}
	}
}

// A relative link in a page resolves against the page's address by the ordinary
// URL rule, so the address must keep the shape that rule works on.
func TestARelativeLinkInACollectionPageResolvesByTheOrdinaryURLRule(t *testing.T) {
	base, _ := url.Parse(collectionAddress("notes", "a/x.md"))
	for ref, want := range map[string]string{
		"b.md":                    "collection://notes/a/b.md",
		"../b.md":                 "collection://notes/b.md",
		"sub/c.md#part":           "collection://notes/a/sub/c.md#part",
		"https://example.org/z":   "https://example.org/z",
		"collection://other/q.md": "collection://other/q.md",
	} {
		r, _ := url.Parse(ref)
		if got := base.ResolveReference(r).String(); got != want {
			t.Errorf("%q resolves to %q, want %q", ref, got, want)
		}
	}
}

func notesFolder(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"top.md":           "# Top\n\nWombats sleep.\n",
		"essays/a b.md":    "# Essay A\n\nQuokkas smile.\n",
		"essays/deep/c.md": "---\ntitle: Deep C\n---\nNumbats eat termites.\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFillCollectionStoresEachFileUnderItsAddressAndRecordsTheFolder(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	root := notesFolder(t)
	rep, err := db.FillCollection(ctx, "notes", root, toc.Limits{})
	if err != nil || rep.Added != 3 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if got := addresses(search(t, db, "quokkas")); len(got) != 1 || got[0] != "collection://notes/essays/a%20b.md" {
		t.Errorf("hits %v", got)
	}
	cols, err := db.Collections(ctx)
	if err != nil || len(cols) != 1 || cols[0].Name != "notes" || cols[0].Root != root {
		t.Errorf("collections %+v, %v", cols, err)
	}
	// the same folder under another name is another collection with other addresses
	if _, err := db.FillCollection(ctx, "copy", root, toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, db, "quokkas").Hits); n != 2 {
		t.Errorf("two collections, %d hits", n)
	}
	// another folder under a taken name is refused and names the stored folder
	_, err = db.FillCollection(ctx, "notes", t.TempDir(), toc.Limits{})
	if !errors.Is(err, ErrCollectionRoot) || !contains(err.Error(), root) {
		t.Errorf("err %v", err)
	}
	for _, bad := range []string{"", "Notes", "a.b", "a/b", "-x", "a b", "x:y", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, err := db.FillCollection(ctx, bad, root, toc.Limits{}); !errors.Is(err, ErrBadCollectionName) {
			t.Errorf("name %q: %v", bad, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestCollectionFileMapsAnAddressToItsFileAndRefusesAnythingElse(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	root := notesFolder(t)
	if _, err := db.FillCollection(ctx, "notes", root, toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	got, err := db.CollectionFile(ctx, "collection://notes/essays/a%20b.md")
	if err != nil || got != filepath.Join(root, "essays", "a b.md") {
		t.Errorf("%q, %v", got, err)
	}
	if got, err := db.CollectionFile(ctx, "collection://notes/top.md#intro"); err != nil || got != filepath.Join(root, "top.md") {
		t.Errorf("an address with a fragment: %q, %v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("secret"), 0o644)
	for _, bad := range []string{
		"collection://notes/../secret.md",
		"collection://notes/essays/../../secret.md",
		"collection://notes/%2e%2e/secret.md",
		"collection://notes/%2E%2E/secret.md",
		"collection://notes/essays/%2e%2e%2f%2e%2e/secret.md",
		"collection://notes//etc/passwd",
		"collection://nobody/top.md",
		"https://notes/top.md",
		"collection:///top.md",
		"notes/top.md",
		"",
	} {
		if got, err := db.CollectionFile(ctx, bad); err == nil {
			t.Errorf("%q gave %q", bad, got)
		}
	}
	if _, err := db.CollectionFile(ctx, "collection://nobody/top.md"); !errors.Is(err, ErrNoCollection) {
		t.Errorf("unknown collection: %v", err)
	}
	// a link added after the fill must not lead a reader out of the folder
	link := filepath.Join(root, "essays", "link.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("no symbolic links here")
	}
	if got, err := db.CollectionFile(ctx, "collection://notes/essays/link.md"); err == nil {
		t.Errorf("a symbolic link out of the folder gave %q", got)
	}
}

func TestForgetOfACollectionRemovesItsRowAndItsSet(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, _ := open(t)
	ctx := context.Background()
	db.FillCollection(ctx, "notes", notesFolder(t), toc.Limits{})
	if n, err := db.Forget(ctx, "notes"); err != nil || n != 3 {
		t.Fatalf("%d, %v", n, err)
	}
	if cols, _ := db.Collections(ctx); len(cols) != 0 {
		t.Errorf("collections %+v", cols)
	}
	if sets, _ := db.Sets(ctx); len(sets) != 0 {
		t.Errorf("sets %+v", sets)
	}
}
