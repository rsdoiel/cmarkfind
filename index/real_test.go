package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// CMARKFIND_REAL_CORPUS names a folder of real Markdown; the index built from
// it must give the same pages for a word as a brute-force scan of the pages.
func TestRealCorpusAgainstBruteForce(t *testing.T) {
	root := os.Getenv("CMARKFIND_REAL_CORPUS")
	if root == "" {
		t.Skip("set CMARKFIND_REAL_CORPUS to a folder of Markdown")
	}
	var docs []Doc
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			if b, err := os.ReadFile(p); err == nil {
				docs = append(docs, Doc{Address: p, Body: string(b)})
			}
		}
		return nil
	})
	dir := t.TempDir()
	if err := Build(dir, docs, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := Verify(dir); err != nil {
		t.Fatal(err)
	}
	ix := open(t, dir)
	t.Logf("%d pages, %d chunks", ix.Count(), ix.Chunks())
	for _, w := range []string{"index", "the", "sqlite", "tokenizer", "zzzz"} {
		var want []string
		for _, d := range docs {
			for _, x := range Words(MarkdownText(d.Body)) {
				if x.Term == w {
					want = append(want, d.Address)
					break
				}
			}
		}
		hits, err := search(ix, w, 0)
		if err != nil {
			t.Fatal(err)
		}
		got := addrs(hits)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%q: index found %d pages, scan found %d", w, len(got), len(want))
		}
	}
}

// The boolean grammar against the brute-force oracle on real pages.
func TestRealCorpusBooleanQueriesAgainstOracle(t *testing.T) {
	root := os.Getenv("CMARKFIND_REAL_CORPUS")
	if root == "" {
		t.Skip("set CMARKFIND_REAL_CORPUS to a folder of Markdown")
	}
	var docs []Doc
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			if b, err := os.ReadFile(p); err == nil {
				title := strings.TrimSuffix(filepath.Base(p), ".md")
				docs = append(docs, Doc{Address: p, Title: title, Body: string(b)})
			}
		}
		return nil
	})
	ix := open(t, build(t, docs, 0))
	oracle := oracleDocs(docs)
	for _, q := range []string{
		"sqlite index", "sqlite OR tokenizer", "sqlite -tokenizer", "(sqlite OR cbor) AND index", "-the",
		`"partitioned index"`, "title:readme", "title:readme -sqlite", "pre* -test*", "body:chunk* OR title:chunk*",
		"NOT sqlite OR NOT index", `"static site" -draft`,
	} {
		n, _ := ParseQuery(q, nil)
		hits, err := ix.Search(n, 0)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, d := range oracle {
			if d.eval(n, "") {
				want = append(want, d.addr)
			}
		}
		got := addrs(hits)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%q: index %d pages, oracle %d", q, len(got), len(want))
		}
		t.Logf("%-32s %d pages", q, len(got))
	}
}
