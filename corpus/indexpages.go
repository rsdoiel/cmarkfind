package corpus

import "context"

/** IndexPage is a stored page as the index writer takes it.
 *
 * Fields:
 *   Address (string)     — the published address
 *   Title (string)       — the page's title
 *   Description (string) — its description
 *   Body (string)        — its Markdown, front matter removed
 *
 * Example:
 *   for _, p := range pages { fmt.Println(p.Address) }
 */
type IndexPage struct {
	Address, Title, Description, Body string
}

/** IndexPages returns the pages an index is built from: every page not marked
 * gone and not under an excluded prefix (the exclusions are applied here, so an
 * excluded page never reaches an index even when its row is still stored).
 * Pages come in address order.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the read
 *
 * Returns:
 *   []IndexPage — the pages; empty when the corpus holds none
 *   error       — database errors
 *
 * Example:
 *   pages, err := db.IndexPages(ctx)
 */
func (d *DB) IndexPages(ctx context.Context) ([]IndexPage, error) {
	skip, err := d.excludedBy(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT address, title, description, body FROM pages WHERE gone_at = '' ORDER BY address`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []IndexPage
	for rows.Next() {
		var p IndexPage
		if err := rows.Scan(&p.Address, &p.Title, &p.Description, &p.Body); err != nil {
			return nil, classify(err)
		}
		if !skip(p.Address) {
			out = append(out, p)
		}
	}
	return out, classify(rows.Err())
}
