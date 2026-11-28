// Package corpus keeps a personal corpus of Markdown pages in one SQLite
// database, fills it from a site's table of contents, and searches it locally
// through a partitioned index built from the pages (cmarkfind DR-0001). The database file is the contract
// between programs: its schema carries a version in the meta table.
package corpus

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	sqlite "github.com/glebarez/go-sqlite"
	"github.com/rsdoiel/cmarkfind/index"
)

// SchemaVersion is the schema this package reads and writes.
const SchemaVersion = 7

var (
	// ErrSchema says the file is not a corpus this package can use: not a
	// database, or a schema newer than SchemaVersion (exit class data, 65).
	ErrSchema = errors.New("corpus: unusable database or schema")
	// ErrBadPrefix says an exclusion prefix is not an http or https address
	// (exit class usage, 2).
	ErrBadPrefix = errors.New("corpus: bad exclusion prefix")
	// ErrLocked says another program holds the database; retrying later may
	// succeed (exit class temp_fail, 75).
	ErrLocked = errors.New("corpus: database is locked")
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS excluded (prefix TEXT PRIMARY KEY, added_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS pins (address TEXT PRIMARY KEY, added_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tags (address TEXT NOT NULL, tag TEXT NOT NULL, added_at TEXT NOT NULL, PRIMARY KEY (address, tag));
CREATE INDEX IF NOT EXISTS tags_by_tag ON tags(tag);
CREATE TABLE IF NOT EXISTS attempts (address TEXT PRIMARY KEY, site TEXT NOT NULL, checked_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sites (site TEXT PRIMARY KEY, listed INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS sets (
	site          TEXT PRIMARY KEY,
	source        TEXT NOT NULL DEFAULT 'local',
	ignored       INTEGER NOT NULL DEFAULT 0,
	fields_version INTEGER NOT NULL DEFAULT 0,
	fields        TEXT NOT NULL DEFAULT '',
	input_digest  TEXT NOT NULL DEFAULT '',
	generation    INTEGER NOT NULL DEFAULT 0,
	document_only TEXT NOT NULL DEFAULT '',
	not_indexed   TEXT NOT NULL DEFAULT '',
	added_at      TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, root TEXT NOT NULL, added_at TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS pages (
	address       TEXT PRIMARY KEY,
	site          TEXT NOT NULL DEFAULT '',
	title         TEXT NOT NULL DEFAULT '',
	description   TEXT NOT NULL DEFAULT '',
	pub_date      TEXT NOT NULL DEFAULT '',
	body          TEXT NOT NULL DEFAULT '',
	hash          TEXT NOT NULL DEFAULT '',
	etag          TEXT NOT NULL DEFAULT '',
	last_modified TEXT NOT NULL DEFAULT '',
	fetched_at    TEXT NOT NULL DEFAULT '',
	checked_at    TEXT NOT NULL DEFAULT '',
	tier          TEXT NOT NULL DEFAULT 'own',
	reader        INTEGER NOT NULL DEFAULT 0,
	gone_at       TEXT NOT NULL DEFAULT '',
	visited_first TEXT NOT NULL DEFAULT '',
	visited_last  TEXT NOT NULL DEFAULT '',
	came_from     TEXT NOT NULL DEFAULT '',
	meta          TEXT NOT NULL DEFAULT ''
);
`

/** DB is an open corpus. It is safe for concurrent use; several programs may
 * open the same file (WAL mode).
 */
type DB struct {
	sql  *sql.DB
	path string

	mu       sync.Mutex
	warnings []string
	report   *index.BuildReport
}

/** Open opens the corpus at path, creating it when the file does not exist.
 *
 * Parameters:
 *   path (string) — the database file
 *
 * Returns:
 *   *DB   — the corpus
 *   error — wraps ErrSchema for a file that is not a corpus or has a newer
 *           schema, ErrLocked when another program holds it
 *
 * Example:
 *   db, err := corpus.Open("corpus.db")
 */
func Open(path string) (*DB, error) {
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, classify(err)
	}
	if _, err := raw.Exec(schema); err != nil {
		raw.Close()
		return nil, classify(err)
	}
	if err := addColumn(raw, "pages", "checked_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		raw.Close()
		return nil, classify(err)
	}
	var v string
	err = raw.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = raw.Exec(`INSERT INTO meta(key, value) VALUES ('schema_version', ?)`, strconv.Itoa(SchemaVersion))
		if err != nil {
			raw.Close()
			return nil, classify(err)
		}
	case err != nil:
		raw.Close()
		return nil, classify(err)
	default:
		n, perr := strconv.Atoi(v)
		if perr != nil || n > SchemaVersion {
			raw.Close()
			return nil, fmt.Errorf("%w: %s has schema version %q, this program reads %d", ErrSchema, path, v, SchemaVersion)
		}
		if n < SchemaVersion { // the new tables were created above; record the upgrade
			if err := upgrade(raw, n); err != nil {
				raw.Close()
				return nil, classify(err)
			}
		}
	}
	return &DB{sql: raw, path: path}, nil
}

// upgrade takes a file from schema version from to SchemaVersion. Version 7
// adds pages.meta, the page's front matter as JSON (empty means not read yet; the
// next fill reads such a page again). Version 6
// adds the sets and collections tables (created with the schema above). Version 5
// drops the FTS5 table and its triggers (search reads the index now); the next
// search builds the index.
func upgrade(raw *sql.DB, from int) error {
	if from < 7 { // the front matter of each page; empty until the page is read again
		if err := addColumn(raw, "pages", "meta", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	tx, err := raw.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if from < 5 {
		for _, q := range []string{`DROP TRIGGER IF EXISTS pages_ai`, `DROP TRIGGER IF EXISTS pages_ad`, `DROP TRIGGER IF EXISTS pages_au`, `DROP TABLE IF EXISTS pages_fts`} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(SchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

/** Close closes the corpus.
 *
 * Returns:
 *   error — from the database
 *
 * Example:
 *   defer db.Close()
 */
func (d *DB) Close() error { return d.sql.Close() }

// classify wraps a driver error in the package's sentinels, by the driver's
// result code and never by message text.
func classify(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return fmt.Errorf("%w: %w", ErrLocked, err)
		case 26, 11: // SQLITE_NOTADB, SQLITE_CORRUPT
			return fmt.Errorf("%w: %w", ErrSchema, err)
		}
	}
	return err
}

// affected returns a result's row count, classifying a driver error.
func affected(res sql.Result, err error) (int, error) {
	if err != nil {
		return 0, classify(err)
	}
	n, err := res.RowsAffected()
	return int(n), classify(err)
}

// addColumn adds a column to a table that an older version created without it.
func addColumn(db *sql.DB, table, column, def string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	have := false
	for rows.Next() {
		var n string
		rows.Scan(&n)
		if n == column {
			have = true
		}
	}
	rows.Close()
	if have {
		return nil
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + def)
	return err
}

// stamp is the time as a fixed-width string that sorts as time does, for
// checked_at (RFC 3339 drops trailing zeros of the fraction, which would not).
func stamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
