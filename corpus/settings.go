package corpus

import (
	"context"
	"errors"
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
	return "", false, errors.New("corpus: Meta is not built")
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
	return errors.New("corpus: SetMeta is not built")
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
	return errors.New("corpus: DeleteMeta is not built")
}
