package corpus

import (
	"context"
	"fmt"
	"strings"
)

/** Hit is one search result.
 *
 * Fields:
 *   Address (string) — the page's published address
 *   Title (string)   — its title
 *   Site (string)    — what the page was filled from
 *   Tier (string)    — how it entered the corpus (own, read, toc, ...)
 *   Snippet (string) — the matching text, matches in [brackets]; empty in a listing
 *   Pinned (bool)    — the person pinned the page
 *   Tags ([]string)  — the person's tags on the page
 *
 * Example:
 *   fmt.Println(h.Title, h.Address, h.Snippet)
 */
type Hit struct {
	Address, Title, Site, Tier, Snippet string
	Pinned                              bool
	Tags                                []string
}

/** Results is the answer to a search.
 *
 * Fields:
 *   Hits ([]Hit)    — best first
 *   Searched (int)  — pages the corpus holds that could have matched
 *   Filtered (int)  — of those, the pages that pass the tag and pinned options; 0 when none was given
 *   Pending (int)   — pages the sites list that have not been fetched yet
 *
 * Example:
 *   fmt.Printf("%d hits, searched %d pages\n", len(r.Hits), r.Searched)
 */
type Results struct {
	Hits     []Hit
	Searched int
	Filtered int
	Pending  int
}

/** SearchOptions are the options of SearchWith.
 *
 * Fields:
 *   Limit (int)     — the most hits to return; 0 means 20
 *   Tags ([]string) — a page must carry every tag given (folded to lower case)
 *   Pinned (bool)   — only pinned pages
 *
 * Example:
 *   o := corpus.SearchOptions{Limit: 10, Tags: []string{"rivers"}}
 */
type SearchOptions struct {
	Limit  int
	Tags   []string
	Pinned bool
}

const hitColumns = `p.address, p.title, p.site, p.tier, EXISTS(SELECT 1 FROM pins k WHERE k.address = p.address)`

const searchSQL = `
SELECT ` + hitColumns + `, snippet(pages_fts, -1, '[', ']', '…', 14)
FROM pages_fts JOIN pages p ON p.rowid = pages_fts.rowid
WHERE pages_fts MATCH ? AND p.gone_at = ''%s
ORDER BY EXISTS(SELECT 1 FROM pins k WHERE k.address = p.address) DESC,
	round(bm25(pages_fts, 4.0, 2.0, 1.0), 2),
	CASE p.tier WHEN 'bookmarked' THEN 0 WHEN 'read' THEN 1 WHEN 'own' THEN 2 WHEN 'toc' THEN 3 ELSE 4 END,
	p.address
LIMIT ?`

const listSQL = `
SELECT ` + hitColumns + `, ''
FROM pages p
WHERE p.gone_at = ''%s
ORDER BY p.title, p.address
LIMIT ?`

/** Search runs query over the corpus. It is SearchWith with only a limit.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the search
 *   query (string)        — what to look for; empty finds nothing
 *   limit (int)           — the most hits to return; 0 means 20
 *
 * Returns:
 *   *Results — the hits and how many pages were searched
 *   error    — from the database only; an unparsable query is not an error
 *
 * Example:
 *   r, err := db.Search(ctx, `"shallow water"`, 10)
 */
func (d *DB) Search(ctx context.Context, query string, limit int) (*Results, error) {
	return d.SearchWith(ctx, query, SearchOptions{Limit: limit})
}

/** SearchWith runs query over the corpus. The query is FTS5's language (words,
 * "phrases", AND, OR, NOT, word*); input that does not parse is searched as
 * quoted words, so punctuation is never a syntax error. Pages marked gone are
 * not searched. Ranking is bm25 with the title weighted most, a page the
 * person chose outranking one harvested in bulk when the scores tie. Pinned
 * matches come first, in that same order. With a tag or pinned option and no
 * query the pages that pass are listed by title, then address, unranked.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the search
 *   query (string)        — what to look for; empty finds nothing unless an option filters
 *   opts (SearchOptions)  — limit, tags (all required), pinned only
 *
 * Returns:
 *   *Results — the hits and how many pages were searched
 *   error    — ErrBadTag for a bad tag; database errors; an unparsable query is not an error
 *
 * Example:
 *   r, err := db.SearchWith(ctx, "otter", corpus.SearchOptions{Tags: []string{"rivers"}})
 */
func (d *DB) SearchWith(ctx context.Context, query string, opts SearchOptions) (*Results, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	frag, fargs, err := filterSQL(opts)
	if err != nil {
		return nil, err
	}
	res := &Results{}
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE gone_at = ''`).Scan(&res.Searched); err != nil {
		return nil, classify(err)
	}
	if res.Pending, err = d.pending(ctx); err != nil {
		return nil, err
	}
	filtered := frag != ""
	if filtered {
		if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pages p WHERE p.gone_at = ''`+frag, fargs...).Scan(&res.Filtered); err != nil {
			return nil, classify(err)
		}
	}
	var hits []Hit
	switch {
	case strings.TrimSpace(query) != "":
		hits, err = d.run(ctx, fmt.Sprintf(searchSQL, frag), append(append([]any{query}, fargs...), limit))
		if err != nil {
			hits, err = d.run(ctx, fmt.Sprintf(searchSQL, frag), append(append([]any{quoteWords(query)}, fargs...), limit)) // not FTS5 syntax: search the words
		}
	case filtered:
		hits, err = d.run(ctx, fmt.Sprintf(listSQL, frag), append(append([]any{}, fargs...), limit))
	default:
		return res, nil
	}
	if err != nil {
		return nil, classify(err)
	}
	for i := range hits {
		if hits[i].Tags, err = d.TagsOf(ctx, hits[i].Address); err != nil {
			return nil, err
		}
	}
	res.Hits = hits
	return res, nil
}

func (d *DB) run(ctx context.Context, q string, args []any) ([]Hit, error) {
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Address, &h.Title, &h.Site, &h.Tier, &h.Pinned, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// quoteWords makes every whitespace-separated word a quoted FTS5 string, a
// quote inside it doubled, so the whole is plain words and no syntax.
func quoteWords(q string) string {
	var out []string
	for _, w := range strings.Fields(q) {
		out = append(out, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	return strings.Join(out, " ")
}

// pendingSQL is, per site, what it lists less what is stored (and not gone) and
// what was asked about and not stored, never below zero.
const pendingSQL = `
SELECT s.site AS site, max(0, s.listed
	- (SELECT count(*) FROM pages p WHERE p.site = s.site AND p.gone_at = '')
	- (SELECT count(*) FROM attempts a WHERE a.site = s.site)) AS pending
FROM sites s`

// pending is the number of listed pages not fetched yet, over every site.
func (d *DB) pending(ctx context.Context) (int, error) {
	var n int
	err := d.sql.QueryRowContext(ctx, `SELECT coalesce(sum(pending), 0) FROM (`+pendingSQL+`)`).Scan(&n)
	return n, classify(err)
}
