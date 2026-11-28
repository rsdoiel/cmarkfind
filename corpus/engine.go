package corpus

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rsdoiel/cmarkfind/index"
)

// ErrIndex says the index could not be written (exit class cant_create, 73).
var ErrIndex = errors.New("corpus: cannot write the index")

// ErrIndexShared says the index folder belongs to another corpus: two corpora
// given one CMARKFIND_INDEX would rebuild over each other on every search (exit
// class config, 78).
var ErrIndexShared = errors.New("corpus: the index folder belongs to another corpus")

const (
	metaCorpusID = "corpus_id"
	ownerFile    = ".cmarkfind-owner"
)

// corpusID is this corpus file's identity, made on first use and kept in meta
// (a copy of the file is the same corpus).
func (d *DB) corpusID(ctx context.Context) (string, error) {
	var id string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaCorpusID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", classify(err)
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	if _, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO meta(key, value) VALUES (?, ?)`, metaCorpusID, hex.EncodeToString(b[:])); err != nil {
		return "", classify(err)
	}
	err = d.sql.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaCorpusID).Scan(&id)
	return id, classify(err)
}

// claim makes the corpus's own index folder this corpus's: a folder with no
// owner file is taken (an index from before folders had owners, or an empty
// one); one another corpus owns is refused.
func (d *DB) claim(ctx context.Context) error {
	if err := d.claimDir(ctx); err != nil {
		return err
	}
	return d.retireTopLevel()
}

func (d *DB) claimDir(ctx context.Context) error {
	id, err := d.corpusID(ctx)
	if err != nil {
		return err
	}
	dir := d.IndexDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	file := filepath.Join(dir, ownerFile)
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = f.WriteString(id + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrIndex, err)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	have, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	if strings.TrimSpace(string(have)) != id {
		return fmt.Errorf("%w: %s; give this corpus a folder of its own with CMARKFIND_INDEX", ErrIndexShared, dir)
	}
	return nil
}

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

func pluralWords(n int) string {
	if n == 1 {
		return "word is"
	}
	return "words are"
}

func pronoun(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

/** IndexWarnings returns, and forgets, the warnings the corpus's own index
 * builds since the last call raised: a set of very common words indexed
 * without positions that differs from the previous build's, and files near
 * the index file limit. Callers print them after the command that rebuilt.
 *
 * Returns:
 *   []string — one line each, no program name; nil when there are none
 *
 * Example:
 *   for _, w := range db.IndexWarnings() { fmt.Fprintln(os.Stderr, "warning:", w) }
 */
func (d *DB) IndexWarnings() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.warnings
	d.warnings = nil
	return w
}

/** LastBuildReport returns the report of the most recent BuildIndex through
 * this handle.
 *
 * Returns:
 *   *index.BuildReport — nil when this handle has not built an index
 *
 * Example:
 *   rep := db.LastBuildReport()
 */
func (d *DB) LastBuildReport() *index.BuildReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.report
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

// afterChange builds the sets whose pages changed (a set whose inputs are as
// they were builds nothing) when changed is true; err passes through.
func (d *DB) afterChange(ctx context.Context, changed bool, err error) error {
	if err != nil || !changed {
		return err
	}
	_, err = d.BuildSets(ctx, "", 0)
	return err
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

// rrfK is the constant of reciprocal rank fusion: a hit's share of the score is
// 1 / (rrfK + its rank in one set), ranks counted from 1.
const rrfK = 60

// searchSets answers a query from the index of each set in sites: every set is
// searched alone and its hits ranked by score (to two decimals), then tier, then
// address; the lists are merged by reciprocal rank fusion, then pinned hits come
// first, then the fused score, tier and address; cut at limit, with a bracketed
// snippet each. The pinned and tag sets filter before ranking. With aggregate,
// a set whose analyzer this program does not read is left out with a warning.
func (d *DB) searchSets(ctx context.Context, sites []string, aggregate bool, query, frag string, fargs []any, limit int, opts SearchOptions) ([]Hit, int, []FacetField, error) {
	// open each set's index first: a query's field clauses are parsed for the
	// union of the fields the sets offer, and a set that lacks one matches nothing for it
	type opened struct {
		site   string
		source string
		ix     *index.Index
	}
	sources, err := d.sourcesOf(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	var firstFail error // the first mirror or reference that could not be read (DR-0017 item 4)
	leftOut := func(site string, err error) {
		d.mu.Lock()
		d.warnings = append(d.warnings, fmt.Sprintf("set %s left out of the search: %v", site, err))
		d.mu.Unlock()
		if firstFail == nil {
			firstFail = err
		}
	}
	var sets []opened
	fieldSet := map[string]bool{}
	var fields, dateFields []string
	dateSeen := map[string]bool{}
	for _, site := range sites {
		ix, err := d.currentSetOpt(ctx, site, opts.NoBuild)
		if aggregate && index.IsAnalyzerError(err) {
			// never mixed: a set in another word rule is left out, and said so
			d.mu.Lock()
			d.warnings = append(d.warnings, fmt.Sprintf("set %v; left out of the search (search-set reads it only when this program knows its analyzer)", err))
			d.mu.Unlock()
			continue
		}
		if err != nil && aggregate && (sources[site] != "local" || errors.Is(err, ErrNeedsBuild)) {
			leftOut(site, err)
			continue
		}
		if err != nil {
			return nil, 0, nil, err
		}
		sets = append(sets, opened{site, sources[site], ix})
		for _, f := range ix.DateFields() {
			if !dateSeen[f] {
				dateSeen[f] = true
				dateFields = append(dateFields, f)
			}
		}
		for _, f := range ix.QueryFields() {
			if !fieldSet[f] {
				fieldSet[f] = true
				fields = append(fields, f)
			}
		}
	}
	if len(sets) == 0 && firstFail != nil {
		return nil, 0, nil, firstFail
	}
	node, err := index.ParseQueryFields(query, fields, dateFields)
	if err != nil {
		return nil, 0, nil, err
	}
	if node == nil {
		return nil, 0, nil, nil
	}
	excluded, err := d.excludedBy(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	readable := 0
	var ok map[string]bool
	if frag != "" {
		if ok, err = d.passing(ctx, frag, fargs); err != nil {
			return nil, 0, nil, err
		}
	}
	type cand struct {
		h         index.Hit
		ix        *index.Index
		fused     float64
		fromIndex bool // a mirror's or reference's hit: its title is the index's
	}
	var all []*cand
	var found4facets []setHits
	meta := map[string]pageMeta{}
	// finish ranks one set's candidates among themselves and adds them to the
	// whole (reciprocal rank fusion: each set is ranked alone, cmarkfind DR-0006 item 8)
	finish := func(mine []*cand, site string, ix *index.Index) {
		score := func(h index.Hit) float64 { return float64(int64(h.Score*100+0.5)) / 100 }
		sort.SliceStable(mine, func(i, j int) bool {
			a, b := mine[i].h, mine[j].h
			if score(a) != score(b) {
				return score(a) > score(b)
			}
			if ra, rb := rankOf(meta[a.Address].tier), rankOf(meta[b.Address].tier); ra != rb {
				return ra < rb
			}
			return a.Address < b.Address
		})
		for i, c := range mine {
			c.fused = 1 / float64(rrfK+i+1)
		}
		docs := make(map[int]bool, len(mine))
		for _, c := range mine {
			docs[c.h.Doc] = true
		}
		found4facets = append(found4facets, setHits{site: site, ix: ix, docs: docs})
		all = append(all, mine...)
	}
	for _, set := range sets {
		ix := set.ix
		found, err := ix.Search(node, 0)
		if err != nil && aggregate && set.source != "local" {
			leftOut(set.site, err)
			continue
		}
		if err != nil {
			return nil, 0, nil, err
		}
		readable++
		if set.source != "local" {
			mine, err := d.publishedHits(ctx, set.site, set.source, found, opts, excluded)
			if err != nil {
				return nil, 0, nil, err
			}
			for _, c := range mine {
				meta[c.Address] = pageMeta{site: set.site, tier: set.source, pinned: c.Pinned}
			}
			cs := make([]*cand, len(mine))
			for i, c := range mine {
				cs[i] = &cand{h: c.Hit, ix: ix, fromIndex: true}
			}
			finish(cs, set.site, ix)
			continue
		}
		if ok != nil {
			kept := found[:0]
			for _, h := range found {
				if ok[h.Address] {
					kept = append(kept, h)
				}
			}
			found = kept
		}
		addrs := make([]string, len(found))
		for i, h := range found {
			addrs[i] = h.Address
		}
		m, err := d.metaOf(ctx, addrs)
		if err != nil {
			return nil, 0, nil, err
		}
		var mine []*cand
		for _, h := range found {
			if pm, live := m[h.Address]; live { // a page marked gone since the build
				meta[h.Address] = pm
				mine = append(mine, &cand{h: h, ix: ix})
			}
		}
		finish(mine, set.site, ix)
	}
	if readable == 0 && firstFail != nil {
		return nil, 0, nil, firstFail
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		ma, mb := meta[a.h.Address], meta[b.h.Address]
		if ma.pinned != mb.pinned {
			return ma.pinned
		}
		if a.fused != b.fused {
			return a.fused > b.fused
		}
		if ra, rb := rankOf(ma.tier), rankOf(mb.tier); ra != rb {
			return ra < rb
		}
		return a.h.Address < b.h.Address
	})
	facets, err := d.facetsOf(ctx, found4facets, query)
	if err != nil {
		return nil, 0, nil, err
	}
	total := len(all) // every match, whatever page is asked for
	from := min(max(opts.Offset, 0), total)
	all = all[from:min(from+limit, total)]
	hits := make([]Hit, 0, len(all))
	for _, c := range all {
		m := meta[c.h.Address]
		snip, anchor, err := snippet(c.ix, node, c.h.Doc)
		if err != nil {
			return nil, 0, nil, err
		}
		title := m.title
		if c.fromIndex {
			if fr, err := c.ix.Fragment(c.h.Doc); err == nil {
				title = fr.Title
			}
		}
		hits = append(hits, Hit{Address: c.h.Address, Title: title, Site: m.site, Tier: m.tier, Snippet: snip, Anchor: anchor, Pinned: m.pinned})
	}
	return hits, total, facets, nil
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
		if i > ex.Start && index.SpaceBetween(words[i-1], words[i]) {
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

// sourcesOf is each set's source, by name.
func (d *DB) sourcesOf(ctx context.Context) (map[string]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT site, source FROM sets`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var site, src string
		if err := rows.Scan(&site, &src); err != nil {
			return nil, classify(err)
		}
		out[site] = src
	}
	return out, classify(rows.Err())
}

// publishedHit is a hit of a mirror or reference with what curation says of its address.
type publishedHit struct {
	index.Hit
	Pinned bool
}

// publishedHits applies curation, by address, to the hits of a published index
// (cmarkfind DR-0017 item 3): an excluded address is dropped, the tag and pinned
// options keep the addresses that pass them, and each hit says whether it is pinned.
// Nothing here needs a row in pages.
func (d *DB) publishedHits(ctx context.Context, site, source string, found []index.Hit, opts SearchOptions, excluded func(string) bool) ([]publishedHit, error) {
	var addrs []string
	var kept []index.Hit
	for _, h := range found {
		if !excluded(h.Address) {
			kept = append(kept, h)
			addrs = append(addrs, h.Address)
		}
	}
	pass, err := d.passByAddress(ctx, opts, addrs)
	if err != nil {
		return nil, err
	}
	pinned, err := d.pinnedAmong(ctx, addrs)
	if err != nil {
		return nil, err
	}
	var out []publishedHit
	for _, h := range kept {
		if pass != nil && !pass[h.Address] {
			continue
		}
		out = append(out, publishedHit{Hit: h, Pinned: pinned[h.Address]})
	}
	return out, nil
}

// byAddress runs a query that selects addresses, for the addresses given, in batches.
func (d *DB) byAddress(ctx context.Context, addrs []string, query string, extra []any) (map[string]bool, error) {
	out := map[string]bool{}
	const batch = 500
	for i := 0; i < len(addrs); i += batch {
		part := addrs[i:min(i+batch, len(addrs))]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, 0, len(part)+len(extra))
		for _, a := range part {
			args = append(args, a)
		}
		args = append(args, extra...)
		rows, err := d.sql.QueryContext(ctx, strings.Replace(query, "(@)", "("+marks+")", 1), args...)
		if err != nil {
			return nil, classify(err)
		}
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				rows.Close()
				return nil, classify(err)
			}
			out[a] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, classify(err)
		}
	}
	return out, nil
}

// pinnedAmong are the addresses of addrs that are pinned.
func (d *DB) pinnedAmong(ctx context.Context, addrs []string) (map[string]bool, error) {
	return d.byAddress(ctx, addrs, `SELECT address FROM pins WHERE address IN (@)`, nil)
}

// passByAddress are the addresses of addrs that pass the tag and pinned options
// (nil when there are none: everything passes), as filterSQL says of pages.
func (d *DB) passByAddress(ctx context.Context, o SearchOptions, addrs []string) (map[string]bool, error) {
	tags, err := foldTags(o.Tags)
	if err != nil {
		return nil, err
	}
	if !o.Pinned && len(tags) == 0 {
		return nil, nil
	}
	pass := map[string]bool{}
	for _, a := range addrs {
		pass[a] = true
	}
	if o.Pinned {
		pins, err := d.pinnedAmong(ctx, addrs)
		if err != nil {
			return nil, err
		}
		for a := range pass {
			if !pins[a] {
				delete(pass, a)
			}
		}
	}
	if len(tags) > 0 {
		sort.Strings(tags)
		marks := strings.TrimSuffix(strings.Repeat("?,", len(tags)), ",")
		args := make([]any, 0, len(tags)+1)
		for _, t := range tags {
			args = append(args, t)
		}
		args = append(args, len(tags))
		all, err := d.byAddress(ctx, addrs, `SELECT address FROM tags WHERE address IN (@) AND tag IN (`+marks+`) GROUP BY address HAVING count(*) = ?`, args)
		if err != nil {
			return nil, err
		}
		for a := range pass {
			if !all[a] {
				delete(pass, a)
			}
		}
	}
	return pass, nil
}
