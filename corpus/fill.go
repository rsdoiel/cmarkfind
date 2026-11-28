package corpus

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsdoiel/markdownweb/page"
	"github.com/rsdoiel/markdownweb/toc"
)

/** FillReport says what a fill did.
 *
 * Fields:
 *   Added, Updated, Unchanged (int) — pages new, changed (or listed again) and left alone
 *   Gone (int)     — pages stored earlier that the outline no longer lists or has no file for
 *   Missing (int)  — pages the outline lists that have no file, or the server says are not there (skipped)
 *   Failed (int)   — pages that could not be read, fetched or parsed (skipped; a stored copy stays)
 *   Forbidden (int) — pages robots.txt keeps out (FillRemote)
 *   Skipped (int)  — pages that answered but are not Markdown (FillRemote)
 *   Excluded (int) — pages an exclusion rule keeps out (not requested, not stored)
 *   Listed (int)   — pages the outline lists, not counting excluded ones
 *   Remaining (int) — listed pages never asked about (FillRemoteWith with a batch)
 *   Placeholder (int) — pages stored under the placeholder host a relative
 *                     outline link resolves to (FillLocal without a SiteURL)
 *   Complete (bool) — the outline was read in full; false when a limit cut it,
 *                     and then nothing is marked gone
 *
 * Example:
 *   fmt.Printf("%d added, %d updated\n", r.Added, r.Updated)
 */
type FillReport struct {
	Added, Updated, Unchanged, Gone, Missing, Failed, Forbidden, Skipped, Excluded, Listed, Remaining, Placeholder int
	Complete                                                                                                       bool
}

/** FillLocal reads the pages a site on disk lists in its table of contents
 * (root/.well-known/toc.opml) and stores them under their published addresses
 * with tier "own". Nothing is fetched. A page whose file is unchanged since the
 * last fill is not written. Pages stored from this root that the outline no
 * longer lists, or that have no file, are marked gone (only when the outline
 * was read in full). It works in one transaction.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fill
 *   root (string)         — the site root, a folder
 *   lim (toc.Limits)      — bounds on the outline walk; zero is the defaults
 *
 * Returns:
 *   *FillReport — what happened (also when err is not nil because of Failed files)
 *   error       — wraps toc.ErrNoOutline when the root has no table of contents;
 *                 the first file error when some files failed
 *
 * Example:
 *   r, err := db.FillLocal(ctx, "/home/me/site", toc.Limits{})
 */
func (d *DB) FillLocal(ctx context.Context, root string, lim toc.Limits) (*FillReport, error) {
	return d.FillLocalWith(ctx, root, lim, FillOptions{})
}

/** FillLocalWith is FillLocal with options; only opts.SiteURL is used. With a
 * SiteURL, every page the outline lists is stored under that address (scheme,
 * host and path prefix) whatever address the outline gave it, which is how a
 * folder whose outline lists relative links or the wrong host is stored under
 * the address it is published at. Exclusion rules are tried against the stored
 * address.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fill
 *   root (string)         — the site root, a folder
 *   lim (toc.Limits)      — bounds on the outline walk; zero is the defaults
 *   opts (FillOptions)    — SiteURL: the published address; empty keeps the outline's
 *
 * Returns:
 *   *FillReport — as FillLocal
 *   error       — as FillLocal; wraps ErrSiteURL when opts.SiteURL is not usable
 *
 * Example:
 *   r, err := db.FillLocalWith(ctx, "/home/me/site", toc.Limits{}, corpus.FillOptions{SiteURL: "https://example.org/"})
 */
func (d *DB) FillLocalWith(ctx context.Context, root string, lim toc.Limits, opts FillOptions) (*FillReport, error) {
	var base *url.URL
	if opts.SiteURL != "" {
		var err error
		if base, err = ParseSiteURL(opts.SiteURL); err != nil {
			return nil, err
		}
	}
	rep, err := d.fillLocal(ctx, root, lim, base)
	return d.afterFill(ctx, rep, err)
}

/** ErrSiteURL is wrapped by ParseSiteURL and FillLocalWith for a published
 * address that is not an http or https address with a host, or that carries a
 * query or a fragment.
 */
var ErrSiteURL = errors.New("not a usable site address")

/** ParseSiteURL checks the address a site is published at.
 *
 * Parameters:
 *   s (string) — an http or https address with a host and, optionally, a path
 *
 * Returns:
 *   *url.URL — the address, its path without a trailing slash
 *   error    — wraps ErrSiteURL
 *
 * Example:
 *   u, err := corpus.ParseSiteURL("https://example.org/blog/") // u.Path == "/blog"
 */
func ParseSiteURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(s, "?") || strings.Contains(s, "#") {
		return nil, fmt.Errorf("%w: %q (want http:// or https:// and a host, optionally a path, no query or fragment)", ErrSiteURL, s)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// placeholderHost is the host markdownweb gives a relative link in a local
// outline.
const placeholderHost = "site.local"

// rebase puts address under base: base's scheme, host and path prefix, then the
// address's own path, query and fragment.
func rebase(address string, base *url.URL) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	u.Scheme, u.Host, u.User = base.Scheme, base.Host, nil
	u.Path = base.Path + u.Path
	u.RawPath = ""
	return u.String()
}

func (d *DB) fillLocal(ctx context.Context, root string, lim toc.Limits, base *url.URL) (*FillReport, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	l, err := toc.NewFinder().PagesLocal(ctx, abs, lim)
	if err != nil {
		return nil, err
	}
	return d.storeLocal(ctx, abs, l, base, &FillReport{})
}

// storeLocal stores the pages of a listing of files on disk under the site key
// abs, marks the site's pages the listing no longer holds as gone (when the
// listing is complete) and records how many pages it listed. rep carries the
// counts a caller already has; it is returned.
func (d *DB) storeLocal(ctx context.Context, abs string, l *toc.Listing, base *url.URL, rep *FillReport) (*FillReport, error) {
	rep.Complete = l.Complete
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, classify(err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	present := map[string]bool{}
	var firstErr error
	excluded, err := d.excludedBy(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range l.Pages {
		if base != nil {
			p.URL = rebase(p.URL, base)
		}
		if excluded(p.URL) {
			rep.Excluded++
			continue
		}
		if u, err := url.Parse(p.URL); base == nil && err == nil && u.Host == placeholderHost {
			rep.Placeholder++
		}
		rep.Listed++
		if p.Path == "" {
			rep.Missing++
			continue
		}
		pg, err := page.ReadFile(p.Path)
		if err != nil {
			rep.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", p.Path, err)
			}
			continue
		}
		present[p.URL] = true
		sum := sha256.Sum256([]byte(pg.Raw))
		hash := hex.EncodeToString(sum[:])
		var oldHash, oldGone, oldMeta string
		err = tx.QueryRowContext(ctx, `SELECT hash, gone_at, meta FROM pages WHERE address = ?`, p.URL).Scan(&oldHash, &oldGone, &oldMeta)
		exists := err == nil
		if exists && oldHash == hash && oldGone == "" && oldMeta != "" { // a page with no meta yet is read again
			rep.Unchanged++
			continue
		}
		title, desc, pub := pg.Title(), descriptionOf(pg), metaStr(pg, "pubDate")
		if pub == "" {
			pub = metaStr(pg, "date")
		}
		if title == "" {
			title = firstHeading(pg.Body)
		}
		if title == "" {
			title = p.Title
		}
		err = upsert(ctx, tx, stored{address: p.URL, site: abs, tier: "own", title: title, desc: desc, pub: pub, body: pg.Body, hash: hash, now: now, checked: stamp(), meta: metaJSON(pg)})
		if err != nil {
			return nil, classify(err)
		}
		if exists {
			rep.Updated++
		} else {
			rep.Added++
		}
	}
	if l.Complete {
		n, err := markGone(ctx, tx, abs, present, now)
		if err != nil {
			return nil, classify(err)
		}
		rep.Gone = n
	}
	if err := setListed(ctx, tx, abs, rep.Listed, now); err != nil {
		return nil, classify(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, classify(err)
	}
	return rep, firstErr
}

// descriptionOf is the page's description, or its abstract when it has none.
func descriptionOf(p *page.Page) string {
	if d := metaStr(p, "description"); d != "" {
		return d
	}
	return metaStr(p, "abstract")
}

// metaScalar is the text of one front matter value, "" for what is not a
// scalar. A YAML date is its ISO 8601 text: the day alone when it has no time.
func metaScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	case map[string]any, map[any]any:
		return ""
	case []any:
		return ""
	}
	return fmt.Sprint(v)
}

// metaJSON is a page's top-level front matter as one JSON object: a string stays
// a string, a number, boolean or date is its text, a list is a list of such
// texts, and anything deeper (a map, a list of maps) is dropped. A page with no
// front matter is "{}", which is how the corpus tells it from "not read yet" ("").
func metaJSON(p *page.Page) string {
	out := map[string]any{}
	for k, v := range p.Meta {
		if list, ok := v.([]any); ok {
			var items []string
			for _, e := range list {
				if t := metaScalar(e); t != "" {
					items = append(items, t)
				}
			}
			if len(items) > 0 {
				out[k] = items
			}
			continue
		}
		if t := metaScalar(v); t != "" {
			out[k] = t
		}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return "{}"
	}
	return strings.TrimSpace(b.String())
}

func metaStr(p *page.Page, key string) string {
	if v, ok := p.Meta[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// firstHeading returns the text of the first "# " line, or "".
func firstHeading(body string) string {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		if t := sc.Text(); strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(t[2:])
		}
	}
	return ""
}

// execer is what a *sql.DB and a *sql.Tx share.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// stored is a page as the corpus keeps it.
type stored struct {
	address, site, tier, title, desc, pub, body, hash, etag, lastModified, now, checked, meta string
}

// upsert writes a page, marking it present again if it had gone.
func upsert(ctx context.Context, q execer, p stored) error {
	_, err := q.ExecContext(ctx, `
INSERT INTO pages(address, site, title, description, pub_date, body, hash, etag, last_modified, fetched_at, checked_at, tier, reader, gone_at, meta)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', ?)
ON CONFLICT(address) DO UPDATE SET site=excluded.site, title=excluded.title, description=excluded.description,
	pub_date=excluded.pub_date, body=excluded.body, hash=excluded.hash, etag=excluded.etag,
	last_modified=excluded.last_modified, fetched_at=excluded.fetched_at, checked_at=excluded.checked_at, tier=excluded.tier, reader=0, gone_at='', meta=excluded.meta`,
		p.address, p.site, p.title, p.desc, p.pub, p.body, p.hash, p.etag, p.lastModified, p.now, p.checked, p.tier, p.meta)
	return err
}

// markGone marks the pages stored from site that are not in present, and
// returns how many.
func markGone(ctx context.Context, q execer, site string, present map[string]bool, now string) (int, error) {
	rows, err := q.QueryContext(ctx, `SELECT address FROM pages WHERE site = ? AND gone_at = ''`, site)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var a string
		rows.Scan(&a)
		if !present[a] {
			gone = append(gone, a)
		}
	}
	rows.Close()
	for _, a := range gone {
		if _, err := q.ExecContext(ctx, `UPDATE pages SET gone_at = ? WHERE address = ?`, now, a); err != nil {
			return 0, err
		}
	}
	return len(gone), nil
}

// setListed records how many pages a site's outline listed at its last fill.
func setListed(ctx context.Context, q execer, site string, listed int, now string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO sites(site, listed, updated_at) VALUES (?, ?, ?)
ON CONFLICT(site) DO UPDATE SET listed = excluded.listed, updated_at = excluded.updated_at`, site, listed, now)
	return err
}
