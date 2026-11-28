package corpus

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/page"
	"github.com/rsdoiel/markdownweb/toc"
)

/** FillOptions are the choices of FillRemoteWith.
 *
 * Fields:
 *   Batch (int) — ask about at most this many pages (cmarkfind DR-0004); 0 is
 *     the whole outline. The pages are the listed ones never asked about, in
 *     outline order, then the rest, the one checked longest ago first.
 *   SiteURL (string) — FillLocalWith only: the address the site is published
 *     at, such as "https://example.org" or "https://example.org/blog/"; every
 *     listed page is stored under it, keeping its path, query and fragment.
 *     Empty stores the addresses as the outline lists them. See ParseSiteURL.
 *
 * Example:
 *   opts := corpus.FillOptions{Batch: 50}
 */
type FillOptions struct {
	Batch   int
	SiteURL string
}

/** FillRemote reads the pages a table of contents on the web lists, all of them.
 * It is FillRemoteWith with no options.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fill; a cancelled fill marks nothing gone
 *   f (*fetch.Fetcher)    — the polite fetcher; nil uses page.Fetcher's
 *   tocURL (string)       — the outline's address, normally <site>/.well-known/toc.opml
 *   lim (toc.Limits)      — bounds on the outline walk; zero is the defaults
 *
 * Returns:
 *   *FillReport — what happened (also when err is not nil)
 *   error       — as FillRemoteWith
 *
 * Example:
 *   r, err := db.FillRemote(ctx, fetch.New(fetch.Options{Pause: time.Second, Robots: true}),
 *       "https://example.org/.well-known/toc.opml", toc.Limits{})
 */
func (d *DB) FillRemote(ctx context.Context, f *fetch.Fetcher, tocURL string, lim toc.Limits) (*FillReport, error) {
	return d.FillRemoteWith(ctx, f, tocURL, lim, FillOptions{})
}

/** FillRemoteWith reads the pages a table of contents on the web lists and
 * stores them under the addresses it lists, tier "toc". Every request goes
 * through f: its pause, User-Agent and robots.txt choice apply. A page already
 * stored is asked for with the ETag and Last-Modified of the last read, so an
 * unchanged page costs a 304 and no change to its text; a page that had gone
 * is asked for plainly. Every page asked about gets a new checked_at.
 *
 * A page the server says is not there (404, 410), or that is not Markdown, or
 * that the outline no longer lists, is marked gone, but only when the outline
 * was read in full and the fill was not cancelled; a listed page a batch did
 * not reach is kept as it is. A page that fails (network, server error, too
 * large, bad front matter) is counted in Failed and its stored copy stays. A
 * page asked about and not stored is remembered, so a dead link takes one
 * place in a batch and then its turn. The network is not held inside a
 * database transaction: each page is written as it arrives.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fill
 *   f (*fetch.Fetcher)    — the polite fetcher; nil uses page.Fetcher's
 *   tocURL (string)       — the outline's address
 *   lim (toc.Limits)      — bounds on the outline walk; zero is the defaults
 *   opts (FillOptions)    — Batch bounds the page requests
 *
 * Returns:
 *   *FillReport — what happened (also when err is not nil); Listed and Remaining count the outline
 *   error       — wraps toc.ErrNoOutline, fetch.ErrUnavailable or fetch.ErrForbidden
 *                 when the outline cannot be read; the context's error when
 *                 cancelled; the first page error when some pages failed
 *
 * Example:
 *   r, err := db.FillRemoteWith(ctx, f, "https://example.org/.well-known/toc.opml", toc.Limits{}, corpus.FillOptions{Batch: 50})
 */
func (d *DB) FillRemoteWith(ctx context.Context, f *fetch.Fetcher, tocURL string, lim toc.Limits, opts FillOptions) (*FillReport, error) {
	rep, err := d.fillRemoteWith(ctx, f, tocURL, lim, opts)
	return d.afterFill(ctx, rep, err)
}

func (d *DB) fillRemoteWith(ctx context.Context, f *fetch.Fetcher, tocURL string, lim toc.Limits, opts FillOptions) (*FillReport, error) {
	if err := d.refuseRemoteSet(ctx, tocURL); err != nil {
		return nil, err
	}
	if f == nil {
		f = page.Fetcher()
	}
	l, err := toc.NewFinderWith(f).Pages(ctx, tocURL, lim)
	if err != nil {
		return nil, err
	}
	rep := &FillReport{Complete: l.Complete}
	now := time.Now().UTC().Format(time.RFC3339)
	excluded, err := d.excludedBy(ctx)
	if err != nil {
		return nil, err
	}

	type item struct {
		p                             toc.Page
		exists, gone, asked           bool
		hash, etag, lm, checked, meta string
	}
	var items []*item
	for _, p := range l.Pages {
		if excluded(p.URL) {
			rep.Excluded++
			continue
		}
		it := &item{p: p}
		var goneAt string
		err := d.sql.QueryRowContext(ctx, `SELECT hash, gone_at, etag, last_modified, checked_at, meta FROM pages WHERE address = ?`, p.URL).
			Scan(&it.hash, &goneAt, &it.etag, &it.lm, &it.checked, &it.meta)
		switch {
		case err == nil:
			it.exists, it.gone, it.asked = true, goneAt != "", true
		case errors.Is(err, sql.ErrNoRows):
			err = d.sql.QueryRowContext(ctx, `SELECT checked_at FROM attempts WHERE address = ?`, p.URL).Scan(&it.checked)
			if err == nil {
				it.asked = true
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, classify(err)
			}
		default:
			return nil, classify(err)
		}
		items = append(items, it)
	}
	rep.Listed = len(items)

	// Which pages to ask about: all, or a batch (DR-0004 2).
	chosen := items
	if opts.Batch > 0 && len(items) > opts.Batch {
		order := append([]*item(nil), items...)
		sort.SliceStable(order, func(i, j int) bool {
			a, b := order[i], order[j]
			if a.asked != b.asked {
				return !a.asked // never asked first, in outline order
			}
			return a.asked && a.checked < b.checked
		})
		chosen = order[:opts.Batch]
	}
	in := map[*item]bool{}
	for _, it := range chosen {
		in[it] = true
	}
	present := map[string]bool{}
	for _, it := range items {
		if !in[it] {
			present[it.p.URL] = it.exists && !it.gone // not reached: keep as it is
		}
	}

	var firstErr error
	fail := func(err error) {
		rep.Failed++
		if firstErr == nil {
			firstErr = err
		}
	}
	// attempt remembers a page that was asked about and is not stored.
	attempt := func(it *item) error {
		if it.exists {
			_, err := d.sql.ExecContext(ctx, `UPDATE pages SET checked_at = ? WHERE address = ?`, stamp(), it.p.URL)
			return err
		}
		_, err := d.sql.ExecContext(ctx, `INSERT INTO attempts(address, site, checked_at) VALUES (?, ?, ?)
ON CONFLICT(address) DO UPDATE SET checked_at = excluded.checked_at, site = excluded.site`, it.p.URL, tocURL, stamp())
		return err
	}
	for _, it := range chosen {
		if err := ctx.Err(); err != nil {
			rep.Complete = false
			return rep, err
		}
		p := it.p
		keep := it.exists && !it.gone // what "present" means if this page fails
		var v fetch.Validators
		if keep && it.meta != "" { // a page whose front matter is not read yet is asked for in full
			v = fetch.Validators{ETag: it.etag, LastModified: it.lm}
		}
		res, err := f.GetIf(ctx, p.URL, page.Accept, v)
		switch {
		case errors.Is(err, fetch.ErrForbidden):
			rep.Forbidden++
			present[p.URL] = keep
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		case err != nil:
			fail(err)
			present[p.URL] = keep
			if ctx.Err() == nil {
				if err := attempt(it); err != nil {
					return rep, classify(err)
				}
			}
			continue
		}
		switch {
		case res.NotModified:
			rep.Unchanged++
			present[p.URL] = true
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		case res.Status == 404 || res.Status == 410:
			rep.Missing++
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		case res.Status < 200 || res.Status > 299:
			fail(&statusError{p.URL, res.Status})
			present[p.URL] = keep
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		}
		if !page.IsMarkdown(res.ContentType, pathOf(p.URL)) {
			rep.Skipped++
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		}
		pg, err := page.Parse(res.Body)
		if err != nil {
			fail(err)
			present[p.URL] = keep
			if err := attempt(it); err != nil {
				return rep, classify(err)
			}
			continue
		}
		present[p.URL] = true
		sum := sha256.Sum256(res.Body)
		hash := hex.EncodeToString(sum[:])
		if it.exists && it.hash == hash && !it.gone && it.meta != "" {
			rep.Unchanged++
			if _, err := d.sql.ExecContext(ctx, `UPDATE pages SET etag = ?, last_modified = ?, checked_at = ? WHERE address = ?`,
				res.ETag, res.LastModified, stamp(), p.URL); err != nil { // same text; new validators and a new check
				return rep, classify(err)
			}
			continue
		}
		title := pg.Title()
		if title == "" {
			title = firstHeading(pg.Body)
		}
		if title == "" {
			title = p.Title
		}
		pub := metaStr(pg, "pubDate")
		if pub == "" {
			pub = metaStr(pg, "date")
		}
		if err := upsert(ctx, d.sql, stored{address: p.URL, site: tocURL, tier: "toc", title: title, desc: descriptionOf(pg),
			pub: pub, body: pg.Body, hash: hash, etag: res.ETag, lastModified: res.LastModified, now: now, checked: stamp(), meta: metaJSON(pg)}); err != nil {
			return rep, classify(err)
		}
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM attempts WHERE address = ?`, p.URL); err != nil {
			return rep, classify(err)
		}
		if it.exists {
			rep.Updated++
		} else {
			rep.Added++
		}
	}
	if ctx.Err() != nil {
		rep.Complete = false
		return rep, ctx.Err()
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return rep, classify(err)
	}
	defer tx.Rollback()
	if l.Complete {
		n, err := markGone(ctx, tx, tocURL, present, now)
		if err != nil {
			return rep, classify(err)
		}
		rep.Gone = n
	}
	if err := setListed(ctx, tx, tocURL, rep.Listed, now); err != nil {
		return rep, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return rep, classify(err)
	}
	// Remaining: listed pages still never asked about.
	for _, it := range items {
		if !it.asked && !in[it] {
			rep.Remaining++
		}
	}
	return rep, firstErr
}

// statusError is a page that answered with a status that is neither success
// nor "not there".
type statusError struct {
	addr   string
	status int
}

func (e *statusError) Error() string { return fmt.Sprintf("%s: HTTP status %d", e.addr, e.status) }

func (e *statusError) Unwrap() error { return fetch.ErrUnavailable }

// pathOf returns the path of an address, for guessing a page's kind from its name.
func pathOf(addr string) string {
	if u, err := url.Parse(addr); err == nil {
		return u.Path
	}
	return addr
}
