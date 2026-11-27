/** Package oracle measures the partitioned index against FTS5 on a corpus.
 *
 * It is the Phase 4 harness: the same questions go through a scratch FTS5
 * table built from the corpus's pages (the corpus itself no longer has one) and
 * through an index built from the same pages, and the report says
 * how far the answers agree (top-k overlap, the sets, the cause of each
 * difference), what each costs (latency, rebuild time, disk) and whether the
 * index agrees with a brute-force scan of the pages. The scan needs neither
 * engine, so it stays as the correctness oracle after FTS5 is gone.
 */
package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/index"
)

/** Query is one question in both engines' words.
 *
 * Fields:
 *   Name (string)  — a short label for the report
 *   FTS (string)   — the query in FTS5's language
 *   Index (string) — the same question in the index's Lucene subset
 *
 * Example:
 *   q := oracle.Query{Name: "not", FTS: "river NOT otter", Index: "river -otter"}
 */
type Query struct {
	Name, FTS, Index string
}

/** Options are the settings of Run.
 *
 * Fields:
 *   K (int)         — how many top hits are compared and timed; 0 means 10
 *   Repeats (int)   — timing runs per query, the median is reported; 0 means 15
 *   DBPath (string) — the corpus file, for the size numbers; "" leaves them 0
 *   ChunkEntries (int) — entries per index chunk; 0 means the default
 *
 * Example:
 *   o := oracle.Options{K: 10, DBPath: "corpus.db"}
 */
type Options struct {
	K            int
	Repeats      int
	DBPath       string
	ChunkEntries int
}

/** Row is the result for one query.
 *
 * Fields:
 *   Query                  — the question
 *   FTSCount, IndexCount (int) — pages each engine found, in full
 *   BruteCount (int)       — pages the scan found
 *   Overlap (float64)      — share of the top-k the two engines share, 1 when both are empty
 *   OnlyFTS, OnlyIndex ([]string) — addresses found by one engine only
 *   Explained (int)        — OnlyFTS pages whose match is in the Markdown source and not in its text (link targets, markup)
 *   Unexplained (int)      — every other page of OnlyFTS and OnlyIndex
 *   IndexVsBrute (int)     — pages the index and the scan disagree on; any is a bug
 *   FTSTime, IndexTime (time.Duration) — median latency of a top-k search (FTS5 includes its snippet)
 *   FTSTop, IndexTop ([]string) — each engine's top-k addresses, best first
 *
 * Example:
 *   fmt.Println(r.Rows[0].Overlap)
 */
type Row struct {
	Query
	FTSCount, IndexCount, BruteCount int
	Overlap                          float64
	OnlyFTS, OnlyIndex               []string
	Explained, Unexplained           int
	IndexVsBrute                     int
	FTSTime, IndexTime               time.Duration
	FTSTop, IndexTop                 []string // the top-k addresses of each engine, best first
}

/** Report is a whole run.
 *
 * Fields:
 *   Pages (int)           — pages indexed
 *   K (int)               — the top-k compared
 *   Rebuild (time.Duration) — best of three index builds from the stored pages
 *   IndexBytes (int64)    — size of the index's files
 *   CorpusBytes (int64)   — size of the corpus file (pages, FTS5 and state)
 *   FTSBytes (int64)      — the FTS5 tables' share of it; 0 when not measurable
 *   Rows ([]Row)          — one per query
 *
 * Example:
 *   r.Write(os.Stdout)
 */
type Report struct {
	Pages       int
	K           int
	Rebuild     time.Duration
	IndexBytes  int64
	CorpusBytes int64
	FTSBytes    int64
	Rows        []Row
}

/** Run answers every query with FTS5, with a freshly built index and with a
 * brute-force scan, and measures both engines.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the run
 *   db (*corpus.DB)       — the corpus; its pages are indexed
 *   qs ([]Query)          — the questions
 *   opt (Options)         — see Options
 *
 * Returns:
 *   *Report — the numbers
 *   error   — a failed build, open or search, or an unparsable Index query
 *
 * Example:
 *   r, err := oracle.Run(ctx, db, qs, oracle.Options{DBPath: path})
 */
func Run(ctx context.Context, db *corpus.DB, qs []Query, opt Options) (*Report, error) {
	if opt.K <= 0 {
		opt.K = 10
	}
	if opt.Repeats <= 0 {
		opt.Repeats = 15
	}
	pages, err := db.IndexPages(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]index.Doc, len(pages))
	for i, p := range pages {
		docs[i] = index.Doc{Address: p.Address, Title: p.Title, Description: p.Description, Body: p.Body}
	}
	tmp, err := os.MkdirTemp("", "oracle-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	r := &Report{Pages: len(pages), K: opt.K}
	var dir string
	for i := 0; i < 3; i++ {
		dir = filepath.Join(tmp, fmt.Sprint("idx", i))
		start := time.Now()
		if err := index.Build(dir, docs, index.BuildOptions{ChunkEntries: opt.ChunkEntries, Generation: 1}); err != nil {
			return nil, err
		}
		if d := time.Since(start); r.Rebuild == 0 || d < r.Rebuild {
			r.Rebuild = d
		}
	}
	r.IndexBytes = dirBytes(dir)
	fts, err := buildFTS(ctx, filepath.Join(tmp, "fts.db"), pages)
	if err != nil {
		return nil, err
	}
	defer fts.db.Close()
	if opt.DBPath != "" {
		r.CorpusBytes = fileBytes(opt.DBPath) + fileBytes(opt.DBPath+"-wal")
	}
	r.FTSBytes = ftsBytes(fts.path)
	ix, err := index.Open(dir)
	if err != nil {
		return nil, err
	}
	for _, q := range qs {
		row, err := one(ctx, fts, ix, pages, q, opt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", q.Name, err)
		}
		r.Rows = append(r.Rows, *row)
	}
	return r, nil
}

const all = 1 << 20

func one(ctx context.Context, fts *ftsRef, ix *index.Index, pages []corpus.IndexPage, q Query, opt Options) (*Row, error) {
	n, err := index.ParseQuery(q.Index, nil)
	if err != nil {
		return nil, err
	}
	row := &Row{Query: q}
	ftsAll, err := fts.search(ctx, q.FTS, all)
	if err != nil {
		return nil, err
	}
	idxHits, err := ix.Search(n, 0)
	if err != nil {
		return nil, err
	}
	ftsHits, idx := ftsAll, []string(nil)
	for _, h := range idxHits {
		idx = append(idx, h.Address)
	}
	brute := Brute(pages, n)
	row.FTSCount, row.IndexCount, row.BruteCount = len(ftsHits), len(idx), len(brute)
	row.Overlap = overlap(ftsHits, idx, opt.K)
	row.FTSTop, row.IndexTop = head(ftsHits, opt.K), head(idx, opt.K)
	row.OnlyFTS, row.OnlyIndex = diff(ftsHits, idx), diff(idx, ftsHits)
	row.IndexVsBrute = len(diff(idx, brute)) + len(diff(brute, idx))
	raw := rawSet(pages, n)
	for _, a := range row.OnlyFTS {
		if raw[a] {
			row.Explained++
		} else {
			row.Unexplained++
		}
	}
	row.Unexplained += len(row.OnlyIndex)
	row.FTSTime = median(opt.Repeats, func() error {
		_, err := fts.search(ctx, q.FTS, opt.K)
		return err
	})
	row.IndexTime = median(opt.Repeats, func() error {
		n, err := index.ParseQuery(q.Index, nil)
		if err != nil {
			return err
		}
		_, err = ix.Search(n, opt.K)
		return err
	})
	return row, nil
}

/** Brute answers a query by scanning the pages' words, with no index. The
 * words are the index's own (Markdown stripped, then the tokenizer), so a
 * difference from the index is a bug in the index and not a tokenizer choice.
 *
 * Parameters:
 *   pages ([]corpus.IndexPage) — the pages
 *   n (*index.Node)            — the parsed query; nil finds nothing
 *
 * Returns:
 *   []string — the matching addresses, sorted
 *
 * Example:
 *   n, _ := index.ParseQuery("otter -river", nil)
 *   addrs := oracle.Brute(pages, n)
 */
func Brute(pages []corpus.IndexPage, n *index.Node) []string {
	return scan(pages, n, index.MarkdownText)
}

// rawSet is the scan over the Markdown as written, the way FTS5 reads a page.
func rawSet(pages []corpus.IndexPage, n *index.Node) map[string]bool {
	out := map[string]bool{}
	for _, a := range scan(pages, n, func(s string) string { return s }) {
		out[a] = true
	}
	return out
}

func scan(pages []corpus.IndexPage, n *index.Node, text func(string) string) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, p := range pages {
		seg := map[string][]string{}
		for name, s := range map[string]string{"title": p.Title, "description": p.Description, "body": p.Body} {
			for _, w := range index.Words(text(s)) {
				seg[name] = append(seg[name], w.Term)
			}
		}
		if eval(seg, n, "") {
			out = append(out, p.Address)
		}
	}
	sort.Strings(out)
	return out
}

func eval(seg map[string][]string, n *index.Node, field string) bool {
	fields := []string{"title", "description", "body"}
	if n.Field != "" {
		field = n.Field
	}
	if field != "" {
		fields = []string{field}
	}
	switch n.Kind {
	case index.KindAnd:
		for _, k := range n.Kids {
			if !eval(seg, k, field) {
				return false
			}
		}
		return true
	case index.KindOr:
		for _, k := range n.Kids {
			if eval(seg, k, field) {
				return true
			}
		}
		return false
	case index.KindNot:
		return !eval(seg, n.Kids[0], field)
	}
	for _, f := range fields { // a term or phrase sits inside one field
		ws := seg[f]
		for i := range ws {
			ok := i+len(n.Words) <= len(ws)
			for k := 0; ok && k < len(n.Words); k++ {
				if k == len(n.Words)-1 && n.Prefix {
					ok = strings.HasPrefix(ws[i+k], n.Words[k])
				} else {
					ok = ws[i+k] == n.Words[k]
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// overlap is the share of the larger top-k list that both lists hold.
func overlap(a, b []string, k int) float64 {
	if len(a) > k {
		a = a[:k]
	}
	if len(b) > k {
		b = b[:k]
	}
	den := len(a)
	if len(b) > den {
		den = len(b)
	}
	if den == 0 {
		return 1
	}
	return float64(len(a)-len(diff(a, b))) / float64(den)
}

func head(a []string, k int) []string {
	if len(a) > k {
		return a[:k]
	}
	return a
}

// diff is the members of a that b lacks, in a's order.
func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	return out
}

func median(n int, f func() error) time.Duration {
	ds := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		if f() != nil {
			return 0
		}
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if d := ds[len(ds)/2]; d > 0 {
		return d
	}
	return 1 // the clock was too coarse; "measured" still means non-zero
}

func fileBytes(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

func dirBytes(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n += fileBytes(p)
		}
		return nil
	})
	return n
}

// ftsRef is the scratch FTS5 table the index is compared with: the pages'
// title, description and body, ranked as the corpus ranked before the switch
// (bm25 weights 4, 2, 1 rounded to two places, then address).
type ftsRef struct {
	db   *sql.DB
	path string
}

func buildFTS(ctx context.Context, path string, pages []corpus.IndexPage) (*ftsRef, error) {
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	if _, err := raw.ExecContext(ctx, `CREATE VIRTUAL TABLE pages_fts USING fts5(address UNINDEXED, title, description, body)`); err != nil {
		raw.Close()
		return nil, err
	}
	tx, err := raw.BeginTx(ctx, nil)
	if err != nil {
		raw.Close()
		return nil, err
	}
	for _, p := range pages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pages_fts(address, title, description, body) VALUES (?, ?, ?, ?)`, p.Address, p.Title, p.Description, p.Body); err != nil {
			tx.Rollback()
			raw.Close()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		raw.Close()
		return nil, err
	}
	return &ftsRef{db: raw, path: path}, nil
}

// search answers an FTS5 query with up to limit addresses, best first; input
// FTS5 cannot parse is searched as quoted words, as the corpus once did.
func (f *ftsRef) search(ctx context.Context, q string, limit int) ([]string, error) {
	const stmt = `SELECT address, snippet(pages_fts, -1, '[', ']', '…', 14) FROM pages_fts WHERE pages_fts MATCH ?
		ORDER BY round(bm25(pages_fts, 0.0, 4.0, 2.0, 1.0), 2), address LIMIT ?`
	rows, err := f.db.QueryContext(ctx, stmt, q, limit)
	if err != nil {
		var words []string
		for _, w := range strings.Fields(q) {
			words = append(words, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
		}
		if rows, err = f.db.QueryContext(ctx, stmt, strings.Join(words, " "), limit); err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, snip string
		if err := rows.Scan(&a, &snip); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ftsBytes is the size of the scratch FTS5 file: the tables hold nothing else.
func ftsBytes(path string) int64 {
	return fileBytes(path)
}

/** Write prints the report as Markdown: the costs, then one row per query.
 *
 * Parameters:
 *   w (io.Writer) — where the report goes
 *
 * Example:
 *   r.Write(os.Stdout)
 */
func (r *Report) Write(w io.Writer) {
	fmt.Fprintf(w, "- pages: %d; top-k: %d\n", r.Pages, r.K)
	fmt.Fprintf(w, "- rebuild (best of 3): %s; index: %d bytes\n", r.Rebuild, r.IndexBytes)
	fmt.Fprintf(w, "- corpus file: %d bytes, scratch FTS5 file for comparison: %d bytes\n\n", r.CorpusBytes, r.FTSBytes)
	fmt.Fprintln(w, "| query | FTS5 | index | brute | overlap | only FTS5 | only index | explained | unexplained | index≠brute | FTS5 time | index time |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, x := range r.Rows {
		fmt.Fprintf(w, "| %s | %d | %d | %d | %.2f | %d | %d | %d | %d | %d | %s | %s |\n",
			x.Name, x.FTSCount, x.IndexCount, x.BruteCount, x.Overlap, len(x.OnlyFTS), len(x.OnlyIndex),
			x.Explained, x.Unexplained, x.IndexVsBrute, x.FTSTime, x.IndexTime)
	}
}
