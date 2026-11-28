package corpus

import (
	"context"
	"sort"
	"strings"
)

/** SiteStat counts the pages that came from one site by one tier.
 *
 * Fields:
 *   Site (string) — the outline address or local root they were filled from
 *   Tier (string) — how they entered the corpus
 *   Pages (int)   — pages that are searchable
 *   Gone (int)    — pages marked gone
 *   Listed (int)  — pages the outline listed at the last fill
 *   Pending (int) — of those, not fetched yet
 *
 * Example:
 *   fmt.Println(s.Site, s.Tier, s.Pages)
 */
type SiteStat struct {
	Site, Tier  string
	Pages, Gone int
	Listed      int // pages the site's outline listed at the last fill
	Pending     int // of those, not fetched yet
}

/** Stats says what a corpus holds.
 *
 * Fields:
 *   Pages (int)         — pages that are searchable
 *   Gone (int)          — pages marked gone
 *   SchemaVersion (int) — the schema the file carries
 *   Excluded (int)      — exclusion rules
 *   Pinned (int)        — pinned addresses
 *   Tags (int)          — distinct tags
 *   TagPages (int)      — tag and page pairs
 *   Pending (int)       — pages the sites list that are not fetched yet
 *   Sites ([]SiteStat)  — counts by site and tier, ordered by site then tier
 *   DocumentOnly ([]string) — very common words the index holds without positions
 *   NotIndexed ([]string)   — addresses the last index build left out because a
 *                             page's text is over one index file's limit; they are
 *                             stored and not searchable
 *
 * Example:
 *   fmt.Printf("%d pages, %d gone\n", st.Pages, st.Gone)
 */
type Stats struct {
	Pages, Gone   int
	SchemaVersion int
	Excluded      int
	Pinned        int // pinned addresses
	Tags          int // distinct tags
	TagPages      int // tag and page pairs
	Pending       int
	Sites         []SiteStat
	DocumentOnly  []string // words the index holds without positions
	NotIndexed    []string // pages the last index build left out: too big for an index file
}

/** Stats counts what the corpus holds.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   *Stats — the counts; zero for an empty corpus
 *   error  — from the database
 *
 * Example:
 *   st, err := db.Stats(ctx)
 */
func (d *DB) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{SchemaVersion: SchemaVersion}
	if err := d.ensureSets(ctx); err != nil {
		return nil, err
	}
	srows, err := d.sql.QueryContext(ctx, `SELECT document_only, not_indexed FROM sets`)
	if err != nil {
		return nil, classify(err)
	}
	only, skipped := map[string]bool{}, map[string]bool{}
	for srows.Next() {
		var o, n string
		if err := srows.Scan(&o, &n); err != nil {
			srows.Close()
			return nil, classify(err)
		}
		for _, w := range strings.Split(o, "\n") {
			if w != "" {
				only[w] = true
			}
		}
		for _, a := range strings.Split(n, "\n") {
			if a != "" {
				skipped[a] = true
			}
		}
	}
	srows.Close()
	for w := range only {
		st.DocumentOnly = append(st.DocumentOnly, w)
	}
	sort.Strings(st.DocumentOnly)
	for a := range skipped {
		st.NotIndexed = append(st.NotIndexed, a)
	}
	sort.Strings(st.NotIndexed)
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM excluded`).Scan(&st.Excluded); err != nil {
		return nil, classify(err)
	}
	if err := d.sql.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pins), (SELECT count(DISTINCT tag) FROM tags), (SELECT count(*) FROM tags)`).Scan(&st.Pinned, &st.Tags, &st.TagPages); err != nil {
		return nil, classify(err)
	}
	rows, err := d.sql.QueryContext(ctx, `
SELECT site, tier, coalesce(sum(gone_at = ''), 0), coalesce(sum(gone_at <> ''), 0)
FROM pages GROUP BY site, tier ORDER BY site, tier`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s SiteStat
		if err := rows.Scan(&s.Site, &s.Tier, &s.Pages, &s.Gone); err != nil {
			return nil, classify(err)
		}
		st.Pages += s.Pages
		st.Gone += s.Gone
		st.Sites = append(st.Sites, s)
	}
	if err := rows.Err(); err != nil {
		return nil, classify(err)
	}
	prow, err := d.sql.QueryContext(ctx, `SELECT s.site, s.listed, p.pending FROM sites s JOIN (`+pendingSQL+`) p ON p.site = s.site`)
	if err != nil {
		return nil, classify(err)
	}
	defer prow.Close()
	seen := map[string]bool{}
	for prow.Next() {
		var site string
		var listed, pend int
		if err := prow.Scan(&site, &listed, &pend); err != nil {
			return nil, classify(err)
		}
		st.Pending += pend
		for i := range st.Sites {
			if st.Sites[i].Site == site {
				st.Sites[i].Listed, st.Sites[i].Pending = listed, pend
				seen[site] = true
			}
		}
		if !seen[site] { // listed, nothing stored yet
			tier := "own"
			if strings.HasPrefix(site, "http") {
				tier = "toc"
			}
			st.Sites = append(st.Sites, SiteStat{Site: site, Tier: tier, Listed: listed, Pending: pend})
		}
	}
	return st, classify(prow.Err())
}
