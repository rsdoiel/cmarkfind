package corpus

import (
	"context"
	"testing"
)

// A page stored under a prefix excluded later (or by another program writing the
// file) must not reach an index; nor may a page marked gone.
func TestIndexPagesSkipExcludedAndGone(t *testing.T) {
	db, _ := open(t)
	twoSites(t, db)
	ctx := context.Background()
	pages, err := db.IndexPages(ctx)
	if err != nil || len(pages) != 3 {
		t.Fatalf("got %d pages, %v", len(pages), err)
	}
	// Excluded after the fact, bypassing Exclude (which would delete the rows).
	if _, err := db.sql.Exec(`INSERT INTO excluded(prefix, added_at) VALUES ('https://other.example/', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`UPDATE pages SET gone_at = 'x' WHERE address LIKE '%a.md'`); err != nil {
		t.Fatal(err)
	}
	pages, err = db.IndexPages(ctx)
	if err != nil || len(pages) != 1 {
		t.Fatalf("got %d pages, %v: %+v", len(pages), err, pages)
	}
	p := pages[0]
	if p.Address == "" || p.Title == "" || p.Body == "" {
		t.Errorf("page not filled: %+v", p)
	}
}
