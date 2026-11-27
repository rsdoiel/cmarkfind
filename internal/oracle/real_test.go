package oracle

import (
	"context"
	"os"
	"testing"

	"github.com/rsdoiel/cmarkfind/corpus"
)

// realQueries are the questions asked of a real corpus: one word of several
// frequencies, prefixes, phrases, and the boolean forms both engines share
// (FTS5 writes NOT between operands where the index writes a minus).
var realQueries = []Query{
	{"rare word", "tokenizer", "tokenizer"},
	{"common word", "index", "index"},
	{"very common word", "the", "the"},
	{"two words", "sqlite index", "sqlite index"},
	{"three words", "partitioned index search", "partitioned index search"},
	{"OR", "sqlite OR tokenizer", "sqlite OR tokenizer"},
	{"NOT", "sqlite NOT tokenizer", "sqlite -tokenizer"},
	{"prefix", "chunk*", "chunk*"},
	{"short prefix", "pre*", "pre*"},
	{"phrase", `"partitioned index"`, `"partitioned index"`},
	{"phrase 3", `"static site"`, `"static site"`},
	{"grouped", "(sqlite OR cbor) AND index", "(sqlite OR cbor) AND index"},
	{"title field", "title: readme", "title:readme"},
	{"mid word", "markdown", "markdown"},
	{"mid word 2", "opml", "opml"},
	{"mid pair", "markdown opml", "markdown opml"},
	{"mid OR", "pagefind OR bm25", "pagefind OR bm25"},
	{"mid prefix", "heading*", "heading*"},
	{"title word", "title: markdown", "title:markdown"},
	{"none", "zzzzqx", "zzzzqx"},
	{"hyphen", `"state-of-the-art"`, `"state-of-the-art"`},
	{"url word", "github", "github"},
}

// CMARKFIND_ORACLE_DB names a corpus file; the report goes to the test log.
func TestRealCorpusReport(t *testing.T) {
	path := os.Getenv("CMARKFIND_ORACLE_DB")
	if path == "" {
		t.Skip("set CMARKFIND_ORACLE_DB to a filled corpus file")
	}
	db, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := Run(context.Background(), db, realQueries, Options{K: 10, Repeats: 3, DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	var b = new(bufWriter)
	r.Write(b)
	t.Logf("\n%s", b.s)
	for _, x := range r.Rows {
		if x.Overlap < 0.5 {
			t.Logf("%s: low overlap\n  FTS5:  %v\n  index: %v", x.Name, x.FTSTop, x.IndexTop)
		}
		if x.IndexVsBrute != 0 {
			t.Errorf("%s: the index and the brute-force scan disagree on %d pages", x.Name, x.IndexVsBrute)
		}
	}
}

type bufWriter struct{ s string }

func (b *bufWriter) Write(p []byte) (int, error) { b.s += string(p); return len(p), nil }
