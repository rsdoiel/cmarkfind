package corpus

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

/** Exclusion is a rule that keeps pages out of the corpus.
 *
 * Fields:
 *   Prefix (string)  — the address prefix: a page whose address starts with it is excluded
 *   AddedAt (string) — when the rule was made, RFC 3339 UTC
 *
 * Example:
 *   fmt.Println(e.Prefix, e.AddedAt)
 */
type Exclusion struct {
	Prefix, AddedAt string
}

/** Forget deletes the pages stored from one site, with their index entries.
 * Exclusions are not touched.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the delete
 *   site (string)         — the site as stored (an outline address or an absolute folder)
 *
 * Returns:
 *   int   — pages deleted; 0 when the site stored nothing
 *   error — from the database
 *
 * Example:
 *   n, err := db.Forget(ctx, "https://example.org/.well-known/toc.opml")
 */
func (d *DB) Forget(ctx context.Context, site string) (int, error) {
	n, err := d.forget(ctx, site)
	if err == nil {
		err = d.dropSets(ctx, []string{site})
	}
	return n, err
}

func (d *DB) forget(ctx context.Context, site string) (int, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, classify(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE site = ?`, site)
	n, err := affected(res, err)
	if err != nil {
		return 0, err
	}
	for _, q := range []string{`DELETE FROM attempts WHERE site = ?`, `DELETE FROM sites WHERE site = ?`, `DELETE FROM collections WHERE name = ?`} {
		if _, err := tx.ExecContext(ctx, q, site); err != nil {
			return 0, classify(err)
		}
	}
	return n, classify(tx.Commit())
}

/** ForgetAll deletes every page. Exclusions are kept.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the delete
 *
 * Returns:
 *   int   — pages deleted
 *   error — from the database
 *
 * Example:
 *   n, err := db.ForgetAll(ctx)
 */
func (d *DB) ForgetAll(ctx context.Context) (int, error) {
	n, err := d.forgetAll(ctx)
	if err == nil {
		err = d.ensureSets(ctx) // no page is left, so no set is
	}
	return n, err
}

func (d *DB) forgetAll(ctx context.Context) (int, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, classify(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM pages`)
	n, err := affected(res, err)
	if err != nil {
		return 0, err
	}
	for _, q := range []string{`DELETE FROM attempts`, `DELETE FROM sites`, `DELETE FROM collections`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return 0, classify(err)
		}
	}
	return n, classify(tx.Commit())
}

/** CountSite counts the pages stored from a site, gone pages included: what
 * Forget would delete. It changes nothing.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   site (string)         — the site as stored
 *
 * Returns:
 *   int   — the count
 *   error — from the database
 *
 * Example:
 *   n, err := db.CountSite(ctx, site)
 */
func (d *DB) CountSite(ctx context.Context, site string) (int, error) {
	var n int
	err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE site = ?`, site).Scan(&n)
	return n, classify(err)
}

/** Exclude records an exclusion and deletes the stored pages it matches, in
 * one transaction. A prefix is plain text: no pattern characters. It is
 * idempotent. End a folder's prefix with a slash, or "/drafts" also matches
 * "/drafts-old".
 *
 * Parameters:
 *   ctx (context.Context) — cancels the call
 *   prefix (string)       — an http or https address, or the start of one
 *
 * Returns:
 *   int   — stored pages deleted
 *   error — for a prefix that is not an http(s) address with a host; from the database
 *
 * Example:
 *   n, err := db.Exclude(ctx, "https://example.org/drafts/")
 */
func (d *DB) Exclude(ctx context.Context, prefix string) (int, error) {
	n, err := d.exclude(ctx, prefix)
	return n, d.afterChange(ctx, n > 0, err)
}

func (d *DB) exclude(ctx context.Context, prefix string) (int, error) {
	if err := validPrefix(prefix); err != nil {
		return 0, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, classify(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO excluded(prefix, added_at) VALUES (?, ?)`,
		prefix, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return 0, classify(err)
	}
	// the sites that listed these pages list that many fewer now
	for _, q := range []string{
		`SELECT site, count(*) FROM pages WHERE substr(address, 1, length(?)) = ? AND gone_at = '' GROUP BY site`,
		`SELECT site, count(*) FROM attempts WHERE substr(address, 1, length(?)) = ? GROUP BY site`,
	} {
		rows, err := tx.QueryContext(ctx, q, prefix, prefix)
		if err != nil {
			return 0, classify(err)
		}
		drop := map[string]int{}
		for rows.Next() {
			var site string
			var c int
			rows.Scan(&site, &c)
			drop[site] = c
		}
		rows.Close()
		for site, c := range drop {
			if _, err := tx.ExecContext(ctx, `UPDATE sites SET listed = max(0, listed - ?) WHERE site = ?`, c, site); err != nil {
				return 0, classify(err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attempts WHERE substr(address, 1, length(?)) = ?`, prefix, prefix); err != nil {
		return 0, classify(err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE substr(address, 1, length(?)) = ?`, prefix, prefix)
	n, err := affected(res, err)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, classify(err)
	}
	return n, nil
}

/** Unexclude removes an exclusion. It brings no page back; the next fill does.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the call
 *   prefix (string)       — the prefix as given to Exclude
 *
 * Returns:
 *   bool  — true when there was such a rule
 *   error — from the database
 *
 * Example:
 *   ok, err := db.Unexclude(ctx, "https://example.org/drafts/")
 */
func (d *DB) Unexclude(ctx context.Context, prefix string) (bool, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM excluded WHERE prefix = ?`, prefix)
	n, err := affected(res, err)
	return n > 0, err
}

/** Excluded lists the exclusions, by prefix.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   []Exclusion — the rules; empty when there are none
 *   error       — from the database
 *
 * Example:
 *   rules, err := db.Excluded(ctx)
 */
func (d *DB) Excluded(ctx context.Context) ([]Exclusion, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT prefix, added_at FROM excluded ORDER BY prefix`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []Exclusion
	for rows.Next() {
		var e Exclusion
		if err := rows.Scan(&e.Prefix, &e.AddedAt); err != nil {
			return nil, classify(err)
		}
		out = append(out, e)
	}
	return out, classify(rows.Err())
}

// excludedBy returns a function that says whether an address has an excluded prefix.
func (d *DB) excludedBy(ctx context.Context) (func(string) bool, error) {
	rules, err := d.Excluded(ctx)
	if err != nil {
		return nil, err
	}
	return func(addr string) bool {
		for _, r := range rules {
			if strings.HasPrefix(addr, r.Prefix) {
				return true
			}
		}
		return false
	}, nil
}

func validPrefix(p string) error {
	u, err := url.Parse(p)
	web := u != nil && (u.Scheme == "http" || u.Scheme == "https")
	coll := u != nil && u.Scheme == "collection" && ValidCollectionName(u.Host)
	if err != nil || !(web || coll) || u.Host == "" || strings.ContainsAny(p, " \t\r\n") {
		return fmt.Errorf("%w: %q is not an http or https address, or a collection:// address", ErrBadPrefix, p)
	}
	return nil
}
