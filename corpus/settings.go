package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNeedsBuild marks a set whose index is missing or not the one this corpus
// last built, in a search that may not build (SearchOptions.NoBuild).
var ErrNeedsBuild = errors.New("corpus: the set's index needs building")

/** Meta returns a value kept in the corpus's meta table.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   key (string)          — the key, such as "server_port"
 *
 * Returns:
 *   string — the value; empty when there is none
 *   bool   — whether the key is stored
 *   error  — from the database
 *
 * Example:
 *   v, ok, err := db.Meta(ctx, "server_port")
 */
func (d *DB) Meta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, classify(err)
	}
	return v, true, nil
}

/** SetMeta stores a value in the meta table, replacing one that is there.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   key, value (string)   — the pair
 *
 * Returns:
 *   error — from the database
 *
 * Example:
 *   err := db.SetMeta(ctx, "server_port", "8123")
 */
func (d *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return classify(err)
}

/** DeleteMeta removes a key from the meta table; a key that is not there is not an error.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   key (string)          — the key
 *
 * Returns:
 *   error — from the database
 *
 * Example:
 *   err := db.DeleteMeta(ctx, "server_port")
 */
func (d *DB) DeleteMeta(ctx context.Context, key string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, key)
	return classify(err)
}

// checkOwner is claim without the writing: the index folder must not belong to
// another corpus. A folder with no owner file has nothing built in it to read.
func (d *DB) checkOwner(ctx context.Context) error {
	dir := d.IndexDir()
	have, err := os.ReadFile(filepath.Join(dir, ownerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	id, _, err := d.Meta(ctx, metaCorpusID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(have)) != id {
		return fmt.Errorf("%w: %s; give this corpus a folder of its own with CMARKFIND_INDEX", ErrIndexShared, dir)
	}
	return nil
}
