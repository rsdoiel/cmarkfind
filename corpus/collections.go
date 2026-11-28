package corpus

// Collections: a named folder of Markdown files with no outline, filled by
// `fill -name NAME FOLDER` and stored under collection://NAME/relative/path.md.
// The name is the set's site key and the host of every address, the path is
// relative and slash-separated on every system (no drive, no root), and the
// folder is kept in the collections table so a program can open a page.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rsdoiel/markdownweb/toc"
)

var (
	// ErrBadCollectionName says a collection name is not 1 to 40 characters of
	// a-z, 0-9, - and _ starting with a letter or digit (exit class usage, 2).
	ErrBadCollectionName = errors.New("corpus: not a usable collection name")
	// ErrCollectionRoot says the name is taken by another folder (exit class
	// usage, 2).
	ErrCollectionRoot = errors.New("corpus: the collection is another folder's")
	// ErrNoCollection says no collection has the name an address gives (exit
	// class no_input, 66).
	ErrNoCollection = errors.New("corpus: no such collection")
)

var collectionName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// maxCollectionFile is the largest file a collection reads; a bigger one is
// skipped with a warning (the fill refuses a page over 8 MiB already).
const maxCollectionFile = 8 << 20

/** ValidCollectionName says whether name can name a collection: 1 to 40
 * characters of a-z, 0-9, - and _, starting with a letter or digit. Such a name
 * has no dot or slash, so it is never a host or an address.
 *
 * Parameters:
 *   name (string) — the candidate
 *
 * Returns:
 *   bool — true when it is usable
 *
 * Example:
 *   ok := corpus.ValidCollectionName("sandbox-writing") // true
 */
func ValidCollectionName(name string) bool { return collectionName.MatchString(name) }

/** Collection is a named folder of Markdown files.
 *
 * Fields:
 *   Name (string) — the collection's name, the host of its addresses
 *   Root (string) — the absolute folder it was filled from
 *
 * Example:
 *   c := corpus.Collection{Name: "notes", Root: "/home/me/notes"}
 */
type Collection struct{ Name, Root string }

/** Collections lists the collections, by name.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   []Collection — every collection; empty when there are none
 *   error        — from the database
 *
 * Example:
 *   cols, err := db.Collections(ctx)
 */
func (d *DB) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT name, root FROM collections ORDER BY name`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []Collection{}
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Name, &c.Root); err != nil {
			return nil, classify(err)
		}
		out = append(out, c)
	}
	return out, classify(rows.Err())
}

/** HasCollection says whether a collection of that name exists.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   name (string)         — the collection's name
 *
 * Returns:
 *   bool  — true when it exists
 *   error — from the database
 *
 * Example:
 *   ok, err := db.HasCollection(ctx, "notes")
 */
func (d *DB) HasCollection(ctx context.Context, name string) (bool, error) {
	var n int
	err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM collections WHERE name = ?`, name).Scan(&n)
	return n > 0, classify(err)
}

// collectionAddress is the address of the file rel (slash separated, relative
// to the collection's folder) in the collection name: each segment is
// percent-encoded as in a URL path.
func collectionAddress(name, rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "collection://" + name + "/" + strings.Join(segs, "/")
}

/** CollectionFile maps a collection:// address to the file it names, through the
 * folder the collection was filled from. It refuses an address that is not a
 * collection address, one with an empty, `.` or `..` segment (also encoded), one
 * whose segment holds a path separator, a collection that does not exist, and a
 * file that, through a symbolic link or otherwise, lies outside the folder. A
 * fragment is ignored.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   address (string)      — for example collection://notes/essays/a%20b.md
 *
 * Returns:
 *   string — the file's path
 *   error  — ErrNoCollection for an unknown name; ErrBadAddress for any other
 *            refusal; fs.ErrNotExist when the file is not there
 *
 * Example:
 *   path, err := db.CollectionFile(ctx, "collection://notes/top.md")
 */
func (d *DB) CollectionFile(ctx context.Context, address string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: %q %s", ErrBadAddress, address, why)
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "collection" {
		return bad("is not a collection address")
	}
	if !ValidCollectionName(u.Host) {
		return bad("names no collection")
	}
	escaped := strings.TrimPrefix(u.EscapedPath(), "/")
	if escaped == "" {
		return bad("names no file")
	}
	var segs []string
	for _, e := range strings.Split(escaped, "/") {
		seg, err := url.PathUnescape(e)
		if err != nil || seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "/\x00") || strings.ContainsRune(seg, filepath.Separator) {
			return bad("has a segment that is not a name")
		}
		segs = append(segs, seg)
	}
	var root string
	err = d.sql.QueryRowContext(ctx, `SELECT root FROM collections WHERE name = ?`, u.Host).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrNoCollection, u.Host)
	}
	if err != nil {
		return "", classify(err)
	}
	path := filepath.Join(append([]string{root}, segs...)...)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return bad("leads out of the collection's folder")
	}
	return path, nil
}

// walkCollection lists the Markdown files under root in path order as a
// listing. It does not enter or read dot files and folders, symbolic links or
// anything but regular files ending in .md or .markdown (any case), and leaves
// out a file over maxCollectionFile. A limit of max pages (0 for none) cuts the
// listing, which is then not complete.
func walkCollection(root, name string, max int, warn func(string, ...any)) (l *toc.Listing, skipped, failed int, first error) {
	l = &toc.Listing{Complete: true}
	links := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
			if e != nil && e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && strings.HasPrefix(e.Name(), ".") {
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e.Type()&fs.ModeSymlink != 0 {
			links++
			return nil
		}
		if e.IsDir() || !e.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
			return nil
		}
		if info.Size() > maxCollectionFile {
			skipped++
			warn("%s is over 8 MiB and is not read", path)
			return nil
		}
		if max > 0 && len(l.Pages) >= max {
			l.Complete = false
			return fs.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		l.Pages = append(l.Pages, toc.Page{
			URL:   collectionAddress(name, filepath.ToSlash(rel)),
			Path:  path,
			Title: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
		})
		return nil
	})
	if err != nil && first == nil {
		first = err
	}
	if links > 0 {
		warn("%d symbolic %s under %s not followed", links, map[bool]string{true: "link", false: "links"}[links == 1], root)
	}
	return l, skipped, failed, first
}

/** FillCollection reads the folder root as the collection name: every Markdown
 * file under it, whatever outline it holds, is stored under
 * collection://NAME/relative/path with tier "own", and the folder is recorded.
 * Pages stored under the name that the walk no longer finds are marked gone,
 * when the walk was complete. The index of the collection's set is built after
 * a fill that changed pages.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fill
 *   name (string)         — the collection's name; see ValidCollectionName
 *   root (string)         — the folder
 *   lim (toc.Limits)      — Pages caps the pages read; the other limits are not used
 *
 * Returns:
 *   *FillReport — what happened (also when err is not nil because of failed files)
 *   error       — ErrBadCollectionName; ErrCollectionRoot, naming the stored
 *                 folder, when the name belongs to another folder; the first
 *                 file or folder error when some failed; database errors
 *
 * Example:
 *   r, err := db.FillCollection(ctx, "sandbox-writing", "/home/me/Sandbox/Writing", toc.Limits{})
 */
func (d *DB) FillCollection(ctx context.Context, name, root string, lim toc.Limits) (*FillReport, error) {
	if !ValidCollectionName(name) {
		return nil, fmt.Errorf("%w: %q (1 to 40 of a-z, 0-9, - and _, starting with a letter or digit)", ErrBadCollectionName, name)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil {
		return nil, err
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a folder", ErrBadAddress, abs)
	}
	var stored string
	err = d.sql.QueryRowContext(ctx, `SELECT root FROM collections WHERE name = ?`, name).Scan(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := d.sql.ExecContext(ctx, `INSERT INTO collections(name, root, added_at) VALUES (?, ?, ?)`, name, abs, stamp()); err != nil {
			return nil, classify(err)
		}
	case err != nil:
		return nil, classify(err)
	case stored != abs:
		return nil, fmt.Errorf("%w: %s holds %s (forget it first to give it another folder)", ErrCollectionRoot, name, stored)
	}
	warn := func(format string, a ...any) {
		d.mu.Lock()
		d.warnings = append(d.warnings, fmt.Sprintf(format, a...))
		d.mu.Unlock()
	}
	l, skipped, failed, first := walkCollection(abs, name, lim.Pages, warn)
	rep, serr := d.storeLocal(ctx, name, l, nil, &FillReport{Skipped: skipped, Failed: failed})
	if rep == nil {
		return nil, serr
	}
	if serr == nil {
		serr = first
	}
	return d.afterFill(ctx, rep, serr)
}
