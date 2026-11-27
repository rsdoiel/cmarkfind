package corpus

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/toc"
)

func many(n int) map[string]string {
	m := map[string]string{}
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("/p%d.md", i)] = fmt.Sprintf("# Page %d\n\nWord%d is here.\n", i, i)
	}
	return m
}

func batch(t *testing.T, db *DB, w *webSite, n int) *FillReport {
	t.Helper()
	r, err := db.FillRemoteWith(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{}, FillOptions{Batch: n})
	if err != nil {
		t.Fatalf("batch: %v (%+v)", err, r)
	}
	return r
}

// asked says which of the pages the server was asked for since the last reset.
func (w *webSite) asked() (out []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for p, n := range w.hits {
		if strings.HasPrefix(p, "/p") && n > 0 {
			out = append(out, p)
		}
	}
	sortStrings(out)
	return out
}
func (w *webSite) reset() { w.mu.Lock(); w.hits = map[string]int{}; w.mu.Unlock() }

func TestABatchAsksAboutExactlyNPagesAndTheNextRunTheNext(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, many(5))
	r := batch(t, db, w, 2)
	if got := fmt.Sprint(w.asked()); r.Added != 2 || r.Listed != 5 || r.Remaining != 3 || got != "[/p1.md /p2.md]" {
		t.Fatalf("run 1: %+v asked %v", r, w.asked())
	}
	w.reset()
	r = batch(t, db, w, 2)
	if r.Added != 2 || r.Remaining != 1 || fmt.Sprint(w.asked()) != "[/p3.md /p4.md]" {
		t.Fatalf("run 2: %+v asked %v", r, w.asked())
	}
	w.reset()
	r = batch(t, db, w, 2)
	if r.Added != 1 || r.Remaining != 0 || r.Gone != 0 {
		t.Fatalf("run 3: %+v asked %v", r, w.asked())
	}
}

// Once everything is stored a batch goes to the pages checked longest ago, and
// a 304 moves a page to the back of the line.
func TestAfterTheSiteIsStoredABatchGoesToTheOldestChecked(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, many(4))
	batch(t, db, w, 2)
	batch(t, db, w, 2)
	w.reset()
	r := batch(t, db, w, 2)
	if fmt.Sprint(w.asked()) != "[/p1.md /p2.md]" || r.Unchanged != 2 {
		t.Fatalf("run 3: %+v asked %v", r, w.asked())
	}
	w.reset()
	batch(t, db, w, 2)
	if fmt.Sprint(w.asked()) != "[/p3.md /p4.md]" {
		t.Errorf("run 4 asked %v", w.asked())
	}
}

func TestADeadLinkTakesOneSlotOnceThenItsTurn(t *testing.T) {
	db, _ := open(t)
	pages := many(4)
	delete(pages, "/p1.md") // listed below, but 404
	w := newWebSite(t, pages)
	w.list("/p1.md", "/p2.md", "/p3.md", "/p4.md")
	var remaining []int
	for i := 0; i < 4; i++ {
		remaining = append(remaining, batch(t, db, w, 1).Remaining)
	}
	if fmt.Sprint(remaining) != "[3 2 1 0]" {
		t.Errorf("remaining %v: the dead link must not hold the batch back", remaining)
	}
	for _, q := range []string{"word2", "word3", "word4"} {
		if len(search(t, db, q).Hits) != 1 {
			t.Errorf("%s not stored", q)
		}
	}
}

func TestAListedPageTheBatchDidNotReachIsKeptAnUnlistedOneIsGone(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, many(3))
	if _, err := db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{}); err != nil {
		t.Fatal(err)
	}
	if r := batch(t, db, w, 1); r.Gone != 0 {
		t.Errorf("a page the batch did not reach was marked gone: %+v", r)
	}
	w.list("/p1.md", "/p2.md")
	if r := batch(t, db, w, 1); r.Gone != 1 {
		t.Errorf("an unlisted page should go: %+v", r)
	}
	if search(t, db, "word3").Searched != 2 || len(search(t, db, "word3").Hits) != 0 {
		t.Error("the unlisted page is still searched")
	}
}

func TestSearchAndStatsSayWhatIsNotFetchedYet(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, many(5))
	batch(t, db, w, 2)
	res := search(t, db, "page")
	if res.Searched != 2 || res.Pending != 3 {
		t.Errorf("search: searched %d pending %d", res.Searched, res.Pending)
	}
	st, _ := db.Stats(context.Background())
	if st.Pending != 3 || len(st.Sites) != 1 || st.Sites[0].Listed != 5 || st.Sites[0].Pending != 3 {
		t.Errorf("stats %+v", st)
	}
	batch(t, db, w, 10)
	if res := search(t, db, "page"); res.Pending != 0 {
		t.Errorf("pending %d after the whole site", res.Pending)
	}
}

func TestForgetClearsTheCountsAndTheAttempts(t *testing.T) {
	db, _ := open(t)
	w := newWebSite(t, many(5))
	batch(t, db, w, 2)
	if n, _ := db.Forget(context.Background(), w.toc()); n != 2 {
		t.Fatalf("forgot %d", n)
	}
	if res := search(t, db, "page"); res.Pending != 0 {
		t.Errorf("pending %d after forget", res.Pending)
	}
	db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{})
	db.ForgetAll(context.Background())
	if res := search(t, db, "page"); res.Pending != 0 {
		t.Errorf("pending %d after forget -all", res.Pending)
	}
}

func TestALocalFillHasNothingPending(t *testing.T) {
	db, _ := open(t)
	fill(t, db, site(t, threePages))
	if res := search(t, db, "the"); res.Pending != 0 {
		t.Errorf("pending %d", res.Pending)
	}
}

func TestAFillWithNoBatchStillWritesCheckedAtAndTheSiteRow(t *testing.T) {
	db, path := open(t)
	w := newWebSite(t, many(3))
	db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{})
	raw, _ := sql.Open("sqlite", path)
	defer raw.Close()
	var n, listed int
	raw.QueryRow(`SELECT count(*) FROM pages WHERE checked_at <> ''`).Scan(&n)
	raw.QueryRow(`SELECT listed FROM sites WHERE site = ?`, w.toc()).Scan(&listed)
	if n != 3 || listed != 3 {
		t.Errorf("checked %d, listed %d", n, listed)
	}
}

func TestAVersion2FileIsUpgradedAndItsPagesAreTheOldestChecked(t *testing.T) {
	db, path := open(t)
	w := newWebSite(t, many(3))
	db.FillRemote(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{})
	db.Close()
	raw, _ := sql.Open("sqlite", path)
	for _, q := range []string{`DROP TABLE attempts`, `DROP TABLE sites`, `ALTER TABLE pages DROP COLUMN checked_at`,
		`UPDATE meta SET value = '2' WHERE key = 'schema_version'`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}
	defer db2.Close()
	if len(search(t, db2, "word1").Hits) != 1 {
		t.Error("pages lost")
	}
	w.reset()
	r, err := db2.FillRemoteWith(context.Background(), fetch.New(fetch.Options{}), w.toc(), toc.Limits{}, FillOptions{Batch: 1})
	if err != nil || r.Unchanged != 1 {
		t.Errorf("batch after upgrade: %+v %v", r, err)
	}
}

func TestExcludeForgetsTheAttemptsUnderThePrefix(t *testing.T) {
	db, _ := open(t)
	pages := many(3)
	delete(pages, "/p1.md")
	w := newWebSite(t, pages)
	w.list("/p1.md", "/p2.md", "/p3.md")
	batch(t, db, w, 3)
	if res := search(t, db, "page"); res.Pending != 0 {
		t.Fatalf("pending %d", res.Pending)
	}
	db.Exclude(context.Background(), w.srv.URL+"/p1.md")
	// the excluded page is no longer listed for the batch, so it cannot be pending
	if r := batch(t, db, w, 3); r.Listed != 2 || r.Excluded != 1 {
		t.Errorf("%+v", r)
	}
}
