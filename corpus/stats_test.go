package corpus

import (
	"context"
	"testing"
)

func TestStatsCountsPagesGoneAndTheSitesAndTiers(t *testing.T) {
	db, _ := open(t)
	st, err := db.Stats(context.Background())
	if err != nil || st.Pages != 0 || st.SchemaVersion != SchemaVersion || len(st.Sites) != 0 {
		t.Fatalf("empty corpus: %+v %v", st, err)
	}
	root := site(t, threePages)
	fill(t, db, root)
	writeOutline(t, root, `<outline text="a" url="https://example.org/a.md"/>`)
	fill(t, db, root)
	st, err = db.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Pages != 1 || st.Gone != 2 {
		t.Errorf("stats %+v", st)
	}
	if len(st.Sites) != 1 || st.Sites[0].Tier != "own" || st.Sites[0].Pages != 1 || st.Sites[0].Gone != 2 {
		t.Errorf("sites %+v", st.Sites)
	}
}
