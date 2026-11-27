package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rsdoiel/cmarkfind/index"
)

// ErrIndex says the index could not be written (exit class cant_create, 73).
var ErrIndex = errors.New("corpus: cannot write the index")

const metaIndexGeneration = "index_generation"

/** DefaultIndexDir returns where the index of a corpus lives when nothing
 * else says: the CMARKFIND_INDEX variable, else an index folder beside the
 * corpus file.
 *
 * Parameters:
 *   dbFile (string) — the corpus file
 *
 * Returns:
 *   string — the directory
 *
 * Example:
 *   d := corpus.DefaultIndexDir("/home/me/.local/share/cmarkfind/corpus.db") // ".../cmarkfind/index"
 */
func DefaultIndexDir(dbFile string) string {
	if d := os.Getenv("CMARKFIND_INDEX"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(dbFile), "index")
}

/** IndexDir is the directory this corpus's index is kept in, as DefaultIndexDir
 * says for the corpus file it was opened from.
 *
 * Returns:
 *   string — the directory
 *
 * Example:
 *   ix, err := index.Open(db.IndexDir())
 */
func (d *DB) IndexDir() string { return DefaultIndexDir(d.path) }

/** BuildIndex writes the index of the stored pages into dir: every page not
 * gone and not under an excluded prefix. The generation goes up by one from
 * the index already there, and files the old manifest named are pruned. When
 * dir is the corpus's own IndexDir the generation is recorded, so a later
 * search can tell this index is the one built from this corpus.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the read of the pages
 *   dir (string)          — the index directory
 *   chunk (int)           — entries per word chunk; 0 means the default
 *
 * Returns:
 *   *index.Index — the opened result
 *   error        — database errors; index.ErrData for a bad page set;
 *                  ErrIndex when the files cannot be written
 *
 * Example:
 *   ix, err := db.BuildIndex(ctx, db.IndexDir(), 0)
 */
func (d *DB) BuildIndex(ctx context.Context, dir string, chunk int) (*index.Index, error) {
	pages, err := d.IndexPages(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]index.Doc, len(pages))
	for i, p := range pages {
		docs[i] = index.Doc{Address: p.Address, Title: p.Title, Description: p.Description, Body: p.Body}
	}
	gen := uint64(1)
	if old, err := index.Open(dir); err == nil {
		gen = old.Generation() + 1
	}
	if err := index.Build(dir, docs, index.BuildOptions{ChunkEntries: chunk, Generation: gen}); err != nil {
		if index.IsDataError(err) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrIndex, err)
	}
	if _, err := index.Prune(dir); err != nil {
		return nil, err
	}
	ix, err := index.Open(dir)
	if err != nil {
		return nil, err
	}
	if dir == d.IndexDir() {
		_, err = d.sql.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			metaIndexGeneration, strconv.FormatUint(ix.Generation(), 10))
		if err != nil {
			return nil, classify(err)
		}
	}
	return ix, nil
}

// afterFill rebuilds the index when a fill changed the pages. The report and
// the fill's own error pass through; a failed rebuild is reported only when the
// fill itself succeeded.
func (d *DB) afterFill(ctx context.Context, rep *FillReport, err error) (*FillReport, error) {
	if rep == nil {
		return rep, err
	}
	if rerr := d.afterChange(ctx, rep.Added+rep.Updated+rep.Gone > 0, nil); rerr != nil && err == nil {
		err = rerr
	}
	return rep, err
}

// afterChange rebuilds the index when changed is true; err passes through.
func (d *DB) afterChange(ctx context.Context, changed bool, err error) error {
	if err != nil || !changed {
		return err
	}
	_, err = d.BuildIndex(ctx, d.IndexDir(), 0)
	return err
}

// current opens the index of this corpus, building it first when it is
// missing or is not the one this corpus last built (another build or another
// corpus wrote the directory).
func (d *DB) current(ctx context.Context) (*index.Index, error) {
	dir := d.IndexDir()
	var rec string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaIndexGeneration).Scan(&rec)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, classify(err)
	}
	if ix, oerr := index.Open(dir); oerr == nil && strconv.FormatUint(ix.Generation(), 10) == rec {
		return ix, nil
	}
	return d.BuildIndex(ctx, dir, 0)
}

// tierRank orders pages of equal score: a page the person chose first.
var tierRank = map[string]int{"bookmarked": 0, "read": 1, "own": 2, "toc": 3}

func rankOf(tier string) int {
	if r, ok := tierRank[tier]; ok {
		return r
	}
	return 4
}

type pageMeta struct {
	title, site, tier string
	pinned            bool
}

// metaOf reads what a hit shows and ranks by, for the given addresses.
func (d *DB) metaOf(ctx context.Context, addrs []string) (map[string]pageMeta, error) {
	out := make(map[string]pageMeta, len(addrs))
	const batch = 500
	for i := 0; i < len(addrs); i += batch {
		end := min(i+batch, len(addrs))
		part := addrs[i:end]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for j, a := range part {
			args[j] = a
		}
		rows, err := d.sql.QueryContext(ctx, `SELECT p.address, p.title, p.site, p.tier,
			EXISTS(SELECT 1 FROM pins k WHERE k.address = p.address)
			FROM pages p WHERE p.gone_at = '' AND p.address IN (`+marks+`)`, args...)
		if err != nil {
			return nil, classify(err)
		}
		for rows.Next() {
			var a string
			var m pageMeta
			if err := rows.Scan(&a, &m.title, &m.site, &m.tier, &m.pinned); err != nil {
				rows.Close()
				return nil, classify(err)
			}
			out[a] = m
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, classify(err)
		}
	}
	return out, nil
}

// passing returns the addresses of live pages that pass the filter fragment.
func (d *DB) passing(ctx context.Context, frag string, args []any) (map[string]bool, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT p.address FROM pages p WHERE p.gone_at = ''`+frag, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, classify(err)
		}
		out[a] = true
	}
	return out, classify(rows.Err())
}

// searchIndex answers a query from the index: every match, filtered by the
// pinned and tag sets, then pinned first, then score (to two decimals), then
// tier, then address, cut at limit, with a bracketed snippet each.
func (d *DB) searchIndex(ctx context.Context, query, frag string, fargs []any, limit int) ([]Hit, error) {
	node, err := index.ParseQuery(query, nil)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}
	ix, err := d.current(ctx)
	if err != nil {
		return nil, err
	}
	all, err := ix.Search(node, 0)
	if err != nil {
		return nil, err
	}
	if frag != "" {
		ok, err := d.passing(ctx, frag, fargs)
		if err != nil {
			return nil, err
		}
		kept := all[:0]
		for _, h := range all {
			if ok[h.Address] {
				kept = append(kept, h)
			}
		}
		all = kept
	}
	addrs := make([]string, len(all))
	for i, h := range all {
		addrs[i] = h.Address
	}
	meta, err := d.metaOf(ctx, addrs)
	if err != nil {
		return nil, err
	}
	live := all[:0]
	for _, h := range all {
		if _, ok := meta[h.Address]; ok { // a page marked gone since the build
			live = append(live, h)
		}
	}
	all = live
	score := func(h index.Hit) float64 { return float64(int64(h.Score*100+0.5)) / 100 }
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		ma, mb := meta[a.Address], meta[b.Address]
		if ma.pinned != mb.pinned {
			return ma.pinned
		}
		if score(a) != score(b) {
			return score(a) > score(b)
		}
		if ra, rb := rankOf(ma.tier), rankOf(mb.tier); ra != rb {
			return ra < rb
		}
		return a.Address < b.Address
	})
	if len(all) > limit {
		all = all[:limit]
	}
	hits := make([]Hit, 0, len(all))
	for _, h := range all {
		m := meta[h.Address]
		snip, anchor, err := snippet(ix, node, h.Doc)
		if err != nil {
			return nil, err
		}
		hits = append(hits, Hit{Address: h.Address, Title: m.title, Site: m.site, Tier: m.tier, Snippet: snip, Anchor: anchor, Pinned: m.pinned})
	}
	return hits, nil
}

// snippetWords is the length of a snippet window.
const snippetWords = 14

// snippet is the best window of the page for the query, each hit word in
// [brackets], an ellipsis where the page goes on, and the slug of the heading
// the first match is under (none for a level 1 heading, the page's title).
func snippet(ix *index.Index, n *index.Node, doc int) (string, string, error) {
	f, err := ix.Fragment(doc)
	if err != nil {
		return "", "", err
	}
	marks, err := ix.Marks(n, doc)
	if err != nil {
		return "", "", err
	}
	ex := index.MakeExcerpt(f, marks, snippetWords)
	hit := map[int]bool{}
	for _, m := range marks {
		hit[m.Pos] = true
	}
	words := strings.Fields(f.Text)
	var b strings.Builder
	if ex.Leading {
		b.WriteString("… ")
	}
	for i := ex.Start; i < ex.End && i < len(words); i++ {
		if i > ex.Start {
			b.WriteByte(' ')
		}
		if hit[i] {
			b.WriteString("[" + words[i] + "]")
		} else {
			b.WriteString(words[i])
		}
	}
	if ex.Trailing {
		b.WriteString(" …")
	}
	anchor := ex.Anchor
	for _, a := range f.Anchors {
		if a.Slug == anchor && a.Level == 1 {
			anchor = "" // the page's own title: the address already says it
		}
	}
	return b.String(), anchor, nil
}
