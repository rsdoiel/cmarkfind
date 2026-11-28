package corpus

import (
	"context"
	"database/sql"
	"testing"
)

// setDirOf is the index folder of the corpus's only set.
func setDirOf(t *testing.T, db *DB) string {
	t.Helper()
	sets, err := db.Sets(context.Background())
	if err != nil || len(sets) != 1 {
		t.Fatalf("want one set, have %d (%v)", len(sets), err)
	}
	return sets[0].IndexDir
}

// openRawFile opens a corpus file with no schema handling, to change it behind
// the package's back.
func openRawFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
