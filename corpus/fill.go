package corpus

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
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
 *   Complete (bool) — the outline was read in full; false when a limit cut it,
 *                     and then nothing is marked gone
 *
 * Example:
 *   fmt.Printf("%d added, %d updated\n", r.Added, r.Updated)
 */
type FillReport struct {
	Added, Updated, Unchanged, Gone, Missing, Failed, Forbidden, Skipped, Excluded, Listed, Remaining int
	Complete                                                                                          bool
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
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	l, err := toc.NewFinder().PagesLocal(ctx, abs, lim)
	if err != nil {
		return nil, err
	}
	rep := &FillReport{Complete: l.Complete}
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
		if excluded(p.URL) {
			rep.Excluded++
			continue
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
		var oldHash, oldGone string
		err = tx.QueryRowContext(ctx, `SELECT hash, gone_at FROM pages WHERE address = ?`, p.URL).Scan(&oldHash, &oldGone)
		exists := err == nil
		if exists && oldHash == hash && oldGone == "" {
			rep.Unchanged++
			continue
		}
		title, desc, pub := pg.Title(), metaStr(pg, "description"), metaStr(pg, "pubDate")
		if pub == "" {
			pub = metaStr(pg, "date")
		}
		if title == "" {
			title = firstHeading(pg.Body)
		}
		if title == "" {
			title = p.Title
		}
		err = upsert(ctx, tx, stored{address: p.URL, site: abs, tier: "own", title: title, desc: desc, pub: pub, body: pg.Body, hash: hash, now: now, checked: stamp()})
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
	address, site, tier, title, desc, pub, body, hash, etag, lastModified, now, checked string
}

// upsert writes a page, marking it present again if it had gone.
func upsert(ctx context.Context, q execer, p stored) error {
	_, err := q.ExecContext(ctx, `
INSERT INTO pages(address, site, title, description, pub_date, body, hash, etag, last_modified, fetched_at, checked_at, tier, reader, gone_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '')
ON CONFLICT(address) DO UPDATE SET site=excluded.site, title=excluded.title, description=excluded.description,
	pub_date=excluded.pub_date, body=excluded.body, hash=excluded.hash, etag=excluded.etag,
	last_modified=excluded.last_modified, fetched_at=excluded.fetched_at, checked_at=excluded.checked_at, tier=excluded.tier, reader=0, gone_at=''`,
		p.address, p.site, p.title, p.desc, p.pub, p.body, p.hash, p.etag, p.lastModified, p.now, p.checked, p.tier)
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
