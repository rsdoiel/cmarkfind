package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Pins and tags are the person's own marks on single pages, kept in tables of
// their own keyed by the page's address.

var (
	// ErrBadAddress says a pin or tag names something that is not an http or
	// https address with a host (exit class usage, 2).
	ErrBadAddress = errors.New("corpus: bad page address")
	// ErrBadTag says a tag is not 1 to 64 letters, digits, - or _ (exit class
	// usage, 2).
	ErrBadTag = errors.New("corpus: bad tag")
)

/** PinState says where a pinned page stands in the corpus. */
type PinState string

const (
	// PinStored is a pinned page the corpus holds and searches.
	PinStored PinState = "stored"
	// PinGone is a pinned page the corpus holds but marked gone.
	PinGone PinState = "gone"
	// PinNotInCorpus is a pinned address the corpus does not hold.
	PinNotInCorpus PinState = "not in the corpus"
)

/** PinInfo is one pinned address.
 *
 * Fields:
 *   Address (string) — the page's published address
 *   Title (string)   — its title, empty when the corpus does not hold it
 *   State (PinState) — stored, gone or not in the corpus
 *   AddedAt (string) — when it was pinned, RFC 3339 UTC
 *
 * Example:
 *   fmt.Println(p.Address, p.State)
 */
type PinInfo struct {
	Address, Title string
	State          PinState
	AddedAt        string
}

/** TagCount is a tag and the number of pages that carry it.
 *
 * Fields:
 *   Tag (string)  — the tag, folded to lower case
 *   Pages (int)   — pages carrying it
 *
 * Example:
 *   fmt.Println(c.Tag, c.Pages)
 */
type TagCount struct {
	Tag   string
	Pages int
}

// validAddress accepts an http or https address with a host and no control
// characters; a pin or tag names a page as search shows it.
func validAddress(a string) error {
	u, err := url.Parse(a)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q is not an http or https address", ErrBadAddress, a)
	}
	return nil
}

// foldTag validates a tag and returns it in lower case.
func foldTag(t string) (string, error) {
	n := utf8.RuneCountInString(t)
	if n < 1 || n > 64 {
		return "", fmt.Errorf("%w: %q must be 1 to 64 characters", ErrBadTag, t)
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return "", fmt.Errorf("%w: %q may hold only letters, digits, - and _", ErrBadTag, t)
		}
	}
	return strings.ToLower(t), nil
}

// foldTags validates and folds tags, dropping repeats.
func foldTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		f, err := foldTag(t)
		if err != nil {
			return nil, err
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out, nil
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

/** Pin marks pages as pinned: a pinned page that matches a search is listed
 * before the others. Pinning an address twice, or one the corpus does not
 * hold, is allowed.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   addresses (...string) — published addresses (http or https)
 *
 * Returns:
 *   int   — addresses newly pinned
 *   error — ErrBadAddress for a bad address (nothing is pinned); database errors
 *
 * Example:
 *   n, err := db.Pin(ctx, "https://example.org/a.md")
 */
func (d *DB) Pin(ctx context.Context, addresses ...string) (int, error) {
	for _, a := range addresses {
		if err := validAddress(a); err != nil {
			return 0, err
		}
	}
	return d.each(ctx, addresses, func(tx *sql.Tx, a string) (sql.Result, error) {
		return tx.ExecContext(ctx, `INSERT OR IGNORE INTO pins(address, added_at) VALUES (?, ?)`, a, nowStamp())
	})
}

/** Unpin removes pins.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   addresses (...string) — published addresses
 *
 * Returns:
 *   int   — pins removed; 0 when none was pinned
 *   error — ErrBadAddress for a bad address; database errors
 *
 * Example:
 *   n, err := db.Unpin(ctx, "https://example.org/a.md")
 */
func (d *DB) Unpin(ctx context.Context, addresses ...string) (int, error) {
	for _, a := range addresses {
		if err := validAddress(a); err != nil {
			return 0, err
		}
	}
	return d.each(ctx, addresses, func(tx *sql.Tx, a string) (sql.Result, error) {
		return tx.ExecContext(ctx, `DELETE FROM pins WHERE address = ?`, a)
	})
}

// each runs one statement per item in one transaction and sums the rows it changed.
func (d *DB) each(ctx context.Context, items []string, run func(*sql.Tx, string) (sql.Result, error)) (int, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, classify(err)
	}
	defer tx.Rollback()
	total := 0
	for _, it := range items {
		n, err := affected(run(tx, it))
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, classify(tx.Commit())
}

/** Pins lists the pinned addresses with where each stands in the corpus.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   []PinInfo — ordered by address; empty when nothing is pinned
 *   error     — from the database
 *
 * Example:
 *   ps, err := db.Pins(ctx)
 */
func (d *DB) Pins(ctx context.Context) ([]PinInfo, error) {
	rows, err := d.sql.QueryContext(ctx, `
SELECT k.address, coalesce(p.title, ''), k.added_at,
	CASE WHEN p.address IS NULL THEN 'n' WHEN p.gone_at <> '' THEN 'g' ELSE 's' END
FROM pins k LEFT JOIN pages p ON p.address = k.address ORDER BY k.address`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []PinInfo
	for rows.Next() {
		var p PinInfo
		var st string
		if err := rows.Scan(&p.Address, &p.Title, &p.AddedAt, &st); err != nil {
			return nil, classify(err)
		}
		p.State = map[string]PinState{"n": PinNotInCorpus, "g": PinGone, "s": PinStored}[st]
		out = append(out, p)
	}
	return out, classify(rows.Err())
}

/** Tag gives a page tags. Tags are folded to lower case, are the person's own
 * and are not part of the searched text.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   address (string)      — the page's published address
 *   tags (...string)      — 1 to 64 letters, digits, - or _ each
 *
 * Returns:
 *   int   — tags newly given
 *   error — ErrBadAddress or ErrBadTag (nothing is written); database errors
 *
 * Example:
 *   n, err := db.Tag(ctx, "https://example.org/a.md", "rivers", "wildlife")
 */
func (d *DB) Tag(ctx context.Context, address string, tags ...string) (int, error) {
	if err := validAddress(address); err != nil {
		return 0, err
	}
	folded, err := foldTags(tags)
	if err != nil {
		return 0, err
	}
	return d.each(ctx, folded, func(tx *sql.Tx, t string) (sql.Result, error) {
		return tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(address, tag, added_at) VALUES (?, ?, ?)`, address, t, nowStamp())
	})
}

/** Untag takes tags off a page.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   address (string)      — the page's published address
 *   tags (...string)      — the tags to remove
 *
 * Returns:
 *   int   — tags removed; 0 when the page carried none of them
 *   error — ErrBadAddress or ErrBadTag; database errors
 *
 * Example:
 *   n, err := db.Untag(ctx, "https://example.org/a.md", "rivers")
 */
func (d *DB) Untag(ctx context.Context, address string, tags ...string) (int, error) {
	if err := validAddress(address); err != nil {
		return 0, err
	}
	folded, err := foldTags(tags)
	if err != nil {
		return 0, err
	}
	return d.each(ctx, folded, func(tx *sql.Tx, t string) (sql.Result, error) {
		return tx.ExecContext(ctx, `DELETE FROM tags WHERE address = ? AND tag = ?`, address, t)
	})
}

/** Tags lists every tag with the number of pages that carry it.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   []TagCount — ordered by tag; empty when there are no tags
 *   error      — from the database
 *
 * Example:
 *   cs, err := db.Tags(ctx)
 */
func (d *DB) Tags(ctx context.Context) ([]TagCount, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT tag, count(*) FROM tags GROUP BY tag ORDER BY tag`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var c TagCount
		if err := rows.Scan(&c.Tag, &c.Pages); err != nil {
			return nil, classify(err)
		}
		out = append(out, c)
	}
	return out, classify(rows.Err())
}

/** TagsOf lists one page's tags.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   address (string)      — the page's published address
 *
 * Returns:
 *   []string — the tags in order; empty when there are none
 *   error    — from the database
 *
 * Example:
 *   ts, err := db.TagsOf(ctx, "https://example.org/a.md")
 */
func (d *DB) TagsOf(ctx context.Context, address string) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT tag FROM tags WHERE address = ? ORDER BY tag`, address)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, classify(err)
		}
		out = append(out, t)
	}
	return out, classify(rows.Err())
}

// filterSQL builds the WHERE fragment for the pinned and tag options; it is
// empty when neither is set.
func filterSQL(o SearchOptions) (frag string, args []any, err error) {
	tags, err := foldTags(o.Tags)
	if err != nil {
		return "", nil, err
	}
	sort.Strings(tags)
	if o.Pinned {
		frag += ` AND p.address IN (SELECT address FROM pins)`
	}
	if len(tags) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(tags)), ",")
		frag += ` AND (SELECT count(*) FROM tags t WHERE t.address = p.address AND t.tag IN (` + marks + `)) = ?`
		for _, t := range tags {
			args = append(args, t)
		}
		args = append(args, len(tags))
	}
	return frag, args, nil
}
