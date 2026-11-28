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
 *   Anchor (string)  — slug of the heading the first match is under (address#Anchor); empty before the first heading, under a level 1 heading (the title) and in a listing
 *   Pinned (bool)    — the person pinned the page
 *   Tags ([]string)  — the person's tags on the page
 *
 * Example:
 *   fmt.Println(h.Title, h.Address, h.Snippet)
 */
type Hit struct {
	Address, Title, Site, Tier, Snippet, Anchor string
	Pinned                                      bool
	Tags                                        []string
}

/** FacetValue is one value of a facet: how many of the pages the search found
 * have it, and the query that searches for the same pages again restricted to it.
 *
 * Fields:
 *   Value (string) — the value, folded to lower case; a year for the year facet
 *   Count (int)    — pages of the whole result that have it
 *   Query (string) — the search's query with the facet's clause added
 *
 * Example:
 *   v := corpus.FacetValue{Value: "go", Count: 12, Query: `(oberon) AND keywords:"go"`}
 */
type FacetValue struct {
	Value string
	Count int
	Query string
}

/** FacetField is a facet of a result: a front matter field the set facets on, its
 * label and its values, most pages first, at most 20.
 *
 * Fields:
 *   Field (string)        — the front matter field, lower case
 *   Label (string)        — what a reader sees: the label the set gave, else the field as typed
 *   Values ([]FacetValue) — its values with counts (the years of a date field)
 *
 * Example:
 *   f := corpus.FacetField{Field: "keywords", Label: "Topics"}
 */
type FacetField struct {
	Field  string
	Label  string
	Values []FacetValue
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
	Facets   []FacetField
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
	Set    string // search only this set (a site key as ResolveSet returns it); empty searches every set not ignored
}

const hitColumns = `p.address, p.title, p.site, p.tier, EXISTS(SELECT 1 FROM pins k WHERE k.address = p.address)`

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

/** SearchWith runs query over the corpus. The query is the index's Lucene subset
 * (words, "phrases", AND, OR, NOT, +, -, word*, title:, description:, body:,
 * parentheses); input that does not parse is read as words, so punctuation is
 * never a syntax error. The index is built after each fill and read here; a
 * missing or foreign one is built first. Pages marked gone are
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
	sites, err := d.setsToSearch(ctx, opts.Set)
	if err != nil {
		return nil, err
	}
	res := &Results{}
	in, inArgs := inList(sites)
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE gone_at = '' AND site IN `+in, inArgs...).Scan(&res.Searched); err != nil {
		return nil, classify(err)
	}
	if n, err := d.publishedPages(ctx, sites); err != nil {
		return nil, err
	} else {
		res.Searched += n
	}
	if res.Pending, err = d.pending(ctx); err != nil {
		return nil, err
	}
	filtered := frag != ""
	if filtered {
		if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pages p WHERE p.gone_at = '' AND p.site IN `+in+frag, append(append([]any{}, inArgs...), fargs...)...).Scan(&res.Filtered); err != nil {
			return nil, classify(err)
		}
	}
	// the sets a search reads bound what a filter lists too
	frag, fargs = " AND p.site IN "+in+frag, append(append([]any{}, inArgs...), fargs...)
	var hits []Hit
	switch {
	case strings.TrimSpace(query) != "":
		hits, res.Facets, err = d.searchSets(ctx, sites, opts.Set == "", query, frag, fargs, limit, opts)
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
