package corpus

import (
	"context"
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
