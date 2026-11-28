package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// relativeSite is a site whose outline lists pages by rooted links, and one
// absolute address on the wrong host.
func relativeSite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for n, body := range map[string]string{"a.md": "# A\n\nOtters.\n", "b/index.md": "# B\n\nBadgers.\n", "c.md": "# C\n\nCranes.\n"} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, n)), 0o755)
		os.WriteFile(filepath.Join(root, n), []byte(body), 0o644)
	}
	os.MkdirAll(filepath.Join(root, ".well-known"), 0o755)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(`<?xml version="1.0"?><opml version="2.0"><head/><body>`+
		`<outline text="a" url="/a.md"/><outline text="b" url="/b/"/><outline text="c" url="http://staging.local:8000/c.md?x=1"/>`+
		`</body></opml>`), 0o644)
	return root
}

func addressesOf(t *testing.T, db string) []string {
	t.Helper()
	raw, err := openRaw(db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rows, err := raw.Query(`SELECT address FROM pages WHERE gone_at = '' ORDER BY address`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		rows.Scan(&a)
		out = append(out, a)
	}
	return out
}

func TestSiteURLRewritesEveryAddressToTheGivenHost(t *testing.T) {
	db := dbPath(t)
	r := do(t, "-db", db, "fill", "-site-url", "https://example.org", relativeSite(t))
	if r.code != 0 || !strings.Contains(r.out, "added 3") {
		t.Fatalf("%+v", r)
	}
	got := strings.Join(addressesOf(t, db), " ")
	want := "https://example.org/a.md https://example.org/b/ https://example.org/c.md?x=1"
	if got != want {
		t.Errorf("addresses\n got %s\nwant %s", got, want)
	}
	if strings.Contains(r.errOut, "site.local") {
		t.Errorf("no warning expected with -site-url: %q", r.errOut)
	}
}

func TestSiteURLPathIsAPrefix(t *testing.T) {
	db := dbPath(t)
	r := do(t, "-db", db, "fill", "-site-url", "https://example.org/blog/", relativeSite(t))
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	got := strings.Join(addressesOf(t, db), " ")
	want := "https://example.org/blog/a.md https://example.org/blog/b/ https://example.org/blog/c.md?x=1"
	if got != want {
		t.Errorf("addresses\n got %s\nwant %s", got, want)
	}
}

func TestWithoutSiteURLAddressesAreStoredAsListedAndPlaceholdersWarn(t *testing.T) {
	// A relative link in the outline resolves against a placeholder host.
	root := relativeSite(t)
	os.WriteFile(filepath.Join(root, ".well-known", "toc.opml"), []byte(`<?xml version="1.0"?><opml version="2.0"><head/><body>`+
		`<outline text="a" url="/a.md"/><outline text="b" url="/b/"/><outline text="c" url="/c.md"/></body></opml>`), 0o644)
	db := dbPath(t)
	r := do(t, "-db", db, "fill", root)
	if r.code != 0 || !strings.Contains(r.out, "added 3") {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.errOut, "cmarkfind:") || !strings.Contains(r.errOut, "-site-url") {
		t.Errorf("want a warning naming -site-url, got %q", r.errOut)
	}
	if strings.Contains(r.out, "-site-url") {
		t.Errorf("warning went to standard output: %q", r.out)
	}
	r = do(t, "-db", dbPath(t), "fill", "-json", root)
	parseJSON(t, r.out)
	if !strings.Contains(r.errOut, "-site-url") {
		t.Errorf("json mode lost the warning: %q", r.errOut)
	}
}

func TestAnAbsoluteOutlineDoesNotWarn(t *testing.T) {
	r := do(t, "-db", dbPath(t), "fill", localSite(t))
	if r.code != 0 || strings.Contains(r.errOut, "-site-url") {
		t.Errorf("%+v", r)
	}
}

func TestRefillingWithAnotherSiteURLMarksTheOldAddressesGone(t *testing.T) {
	db, site := dbPath(t), relativeSite(t)
	do(t, "-db", db, "fill", "-site-url", "https://one.example", site)
	r := do(t, "-db", db, "fill", "-site-url", "https://two.example", site)
	if r.code != 0 || !strings.Contains(r.out, "added 3") || !strings.Contains(r.out, "gone 3") {
		t.Fatalf("%+v", r)
	}
	for _, a := range addressesOf(t, db) {
		if !strings.HasPrefix(a, "https://two.example/") {
			t.Errorf("live address %s", a)
		}
	}
}

func TestSiteURLBadValuesAndRemoteSourcesAreUsageErrors(t *testing.T) {
	site := relativeSite(t)
	srv := webSite(t)
	for _, c := range [][]string{
		{"fill", "-site-url", "example.org", site},
		{"fill", "-site-url", "ftp://example.org", site},
		{"fill", "-site-url", "https://", site},
		{"fill", "-site-url", "https://example.org/?q=1", site},
		{"fill", "-site-url", "https://example.org/#top", site},
		{"fill", "-site-url", "https://example.org", srv.URL},
	} {
		args := append([]string{"-db", dbPath(t)}, c...)
		r := do(t, args...)
		if r.code != 2 || strings.Contains(r.errOut, "not defined") || !strings.Contains(r.errOut, "site-url") {
			t.Errorf("%v: code %d, want 2 from -site-url itself (%q)", c[1:3], r.code, r.errOut)
		}
	}
}
