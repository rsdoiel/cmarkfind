package corpus

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

// Tests for pin and tag, written before the code: they fail
// to compile until Pin, Unpin, Pins, Tag, Untag, Tags, TagsOf, SearchWith,
// SearchOptions, Hit.Pinned, Hit.Tags, Results.Filtered and Stats.Pinned exist.

const (
	strongAddr = "https://example.org/strong.md"
	weakAddr   = "https://example.org/weak.md"
	otherAddr  = "https://example.org/other.md"
)

// rankSite has two pages that match "otter" (strong far better than weak) and
// one that does not.
func rankSite(t *testing.T) string {
	return site(t, map[string]*string{
		"strong.md": s("# Otter otter\n\nOtter otter otter otter.\n"),
		"weak.md":   s("# Rivers\n\nOnce an otter swam by in a long story about rivers, fields, hills and other matters.\n"),
		"other.md":  s("# Badgers\n\nBadgers dig setts.\n"),
	})
}

func rankDB(t *testing.T) *DB {
	t.Helper()
	db, _ := open(t)
	fill(t, db, rankSite(t))
	return db
}

func searchWith(t *testing.T, db *DB, q string, o SearchOptions) *Results {
	t.Helper()
	r, err := db.SearchWith(context.Background(), q, o)
	if err != nil {
		t.Fatalf("searchwith %q: %v", q, err)
	}
	return r
}

func TestAPinnedMatchRanksAboveABetterUnpinnedMatch(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	if got := addresses(search(t, db, "otter")); !reflect.DeepEqual(got, []string{strongAddr, weakAddr}) {
		t.Fatalf("unpinned order %v", got)
	}
	if n, err := db.Pin(ctx, weakAddr); err != nil || n != 1 {
		t.Fatalf("pin: %d, %v", n, err)
	}
	r := search(t, db, "otter")
	if got := addresses(r); !reflect.DeepEqual(got, []string{weakAddr, strongAddr}) {
		t.Fatalf("pinned order %v", got)
	}
	if !r.Hits[0].Pinned || r.Hits[1].Pinned {
		t.Errorf("pinned flags %+v", r.Hits)
	}
}

func TestTwoPinnedMatchesKeepRankOrderBetweenThem(t *testing.T) {
	db := rankDB(t)
	db.Pin(context.Background(), weakAddr, strongAddr)
	if got := addresses(search(t, db, "otter")); !reflect.DeepEqual(got, []string{strongAddr, weakAddr}) {
		t.Errorf("order %v", got)
	}
}

func TestAPinOnANonMatchingPageChangesNothing(t *testing.T) {
	db := rankDB(t)
	db.Pin(context.Background(), otherAddr)
	r := search(t, db, "otter")
	if got := addresses(r); !reflect.DeepEqual(got, []string{strongAddr, weakAddr}) {
		t.Errorf("order %v", got)
	}
	if r.Searched != 3 {
		t.Errorf("searched %d", r.Searched)
	}
}

func TestUnpinRestoresTheOrder(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Pin(ctx, weakAddr)
	if n, err := db.Unpin(ctx, weakAddr); err != nil || n != 1 {
		t.Fatalf("unpin: %d, %v", n, err)
	}
	if got := addresses(search(t, db, "otter")); !reflect.DeepEqual(got, []string{strongAddr, weakAddr}) {
		t.Errorf("order %v", got)
	}
	if n, err := db.Unpin(ctx, weakAddr); err != nil || n != 0 {
		t.Errorf("second unpin: %d, %v", n, err)
	}
}

func TestPinIsIdempotent(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Pin(ctx, weakAddr)
	if n, err := db.Pin(ctx, weakAddr); err != nil || n != 0 {
		t.Errorf("second pin: %d, %v", n, err)
	}
	if ps, _ := db.Pins(ctx); len(ps) != 1 {
		t.Errorf("pins %+v", ps)
	}
}

func TestAPinOnAnAddressNotYetStoredAppliesOnceTheFillBringsThePage(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if n, err := db.Pin(ctx, weakAddr); err != nil || n != 1 {
		t.Fatalf("pin: %d, %v", n, err)
	}
	ps, _ := db.Pins(ctx)
	if len(ps) != 1 || ps[0].State != PinNotInCorpus {
		t.Fatalf("pins %+v", ps)
	}
	fill(t, db, rankSite(t))
	if got := addresses(search(t, db, "otter")); !reflect.DeepEqual(got, []string{weakAddr, strongAddr}) {
		t.Errorf("order %v", got)
	}
	if ps, _ := db.Pins(ctx); ps[0].State != PinStored || ps[0].Title != "Rivers" {
		t.Errorf("pins %+v", ps)
	}
}

func TestPinsAndTagsSurviveARefillAForgetAndAPageGoingGone(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	root := rankSite(t)
	fill(t, db, root)
	db.Pin(ctx, weakAddr)
	db.Tag(ctx, weakAddr, "rivers")
	fill(t, db, root) // refill
	if got := addresses(search(t, db, "otter")); got[0] != weakAddr {
		t.Errorf("after refill %v", got)
	}
	if _, err := db.ForgetAll(ctx); err != nil {
		t.Fatal(err)
	}
	if ps, _ := db.Pins(ctx); len(ps) != 1 {
		t.Errorf("forget removed the pin: %+v", ps)
	}
	if ts, _ := db.TagsOf(ctx, weakAddr); !reflect.DeepEqual(ts, []string{"rivers"}) {
		t.Errorf("forget removed the tag: %v", ts)
	}
	fill(t, db, root)
	if got := addresses(search(t, db, "otter")); got[0] != weakAddr {
		t.Errorf("after forget and fill %v", got)
	}
	// gone: a page the outline stops listing is marked gone, then returns
	writeOutline(t, root, `<outline text="s" url="https://example.org/strong.md"/><outline text="o" url="https://example.org/other.md"/>`)
	fill(t, db, root) // weak is no longer listed: gone
	if ps, _ := db.Pins(ctx); len(ps) != 1 || ps[0].State != PinGone {
		t.Errorf("gone pin %+v", ps)
	}
	if got := addresses(search(t, db, "otter")); !reflect.DeepEqual(got, []string{strongAddr}) {
		t.Errorf("a gone page was searched: %v", got)
	}
	writeOutline(t, root, `<outline text="s" url="https://example.org/strong.md"/><outline text="w" url="https://example.org/weak.md"/>`)
	fill(t, db, root) // listed again: it comes back
	if got := addresses(search(t, db, "otter")); got[0] != weakAddr {
		t.Errorf("after returning %v", got)
	}
}

func rankSiteBody(name string) *string {
	switch name {
	case "strong.md":
		return s("# Otter otter\n\nOtter otter otter otter.\n")
	case "weak.md":
		return s("# Rivers\n\nOnce an otter swam by in a long story about rivers, fields, hills and other matters.\n")
	}
	return s("# Badgers\n\nBadgers dig setts.\n")
}

func TestExcludeThenUnexcludeAndFillBringsThePinBack(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Pin(ctx, weakAddr)
	db.Exclude(ctx, weakAddr)
	if ps, _ := db.Pins(ctx); len(ps) != 1 || ps[0].State != PinNotInCorpus {
		t.Fatalf("pins %+v", ps)
	}
	db.Unexclude(ctx, weakAddr)
	fill(t, db, rankSite(t))
	if got := addresses(search(t, db, "otter")); got[0] != weakAddr {
		t.Errorf("order %v", got)
	}
}

func TestPinRejectsWhatIsNotAnAddress(t *testing.T) {
	db, _ := open(t)
	for _, a := range []string{"", "example.org/x", "ftp://example.org/x", "https://", "/local/path"} {
		if _, err := db.Pin(context.Background(), a); !errors.Is(err, ErrBadAddress) {
			t.Errorf("%q: %v", a, err)
		}
	}
}

func TestTagsAreFoldedValidatedAndPerPage(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	if n, err := db.Tag(ctx, strongAddr, "Otters", "river_life", "Ünï-code"); err != nil || n != 3 {
		t.Fatalf("tag: %d, %v", n, err)
	}
	ts, _ := db.TagsOf(ctx, strongAddr)
	if !reflect.DeepEqual(ts, []string{"river_life", "otters", "ünï-code"}) && !reflect.DeepEqual(ts, []string{"otters", "river_life", "ünï-code"}) {
		t.Errorf("tags %v", ts)
	}
	if n, _ := db.Tag(ctx, strongAddr, "OTTERS"); n != 0 {
		t.Errorf("a folded duplicate counted %d", n)
	}
	for _, bad := range []string{"", "has space", "semi;colon", "esc\x1b[31m", string(make([]byte, 65)), "a/b"} {
		if _, err := db.Tag(ctx, strongAddr, bad); !errors.Is(err, ErrBadTag) {
			t.Errorf("tag %q: %v", bad, err)
		}
	}
	if _, err := db.Tag(ctx, "not-an-address", "ok"); !errors.Is(err, ErrBadAddress) {
		t.Errorf("bad address: %v", err)
	}
	long := ""
	for i := 0; i < 64; i++ {
		long += "x"
	}
	if _, err := db.Tag(ctx, strongAddr, long); err != nil {
		t.Errorf("64 characters refused: %v", err)
	}
}

func TestUntagRemovesOnlyWhatWasThere(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Tag(ctx, strongAddr, "a", "b")
	if n, err := db.Untag(ctx, strongAddr, "a", "zzz"); err != nil || n != 1 {
		t.Errorf("untag: %d, %v", n, err)
	}
	if n, _ := db.Untag(ctx, strongAddr, "a"); n != 0 {
		t.Errorf("again: %d", n)
	}
	if ts, _ := db.TagsOf(ctx, strongAddr); !reflect.DeepEqual(ts, []string{"b"}) {
		t.Errorf("tags %v", ts)
	}
}

func TestTagsListsEveryTagWithItsPageCount(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Tag(ctx, strongAddr, "wet", "furry")
	db.Tag(ctx, weakAddr, "wet")
	got, err := db.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"furry", 1}, {"wet", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags %+v", got)
	}
}

func TestATagFilterKeepsOnlyPagesWithEveryTagGiven(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Tag(ctx, strongAddr, "wet", "furry")
	db.Tag(ctx, weakAddr, "wet")
	r := searchWith(t, db, "otter", SearchOptions{Tags: []string{"wet"}})
	if len(r.Hits) != 2 || r.Searched != 3 || r.Filtered != 2 {
		t.Errorf("one tag: %d hits, searched %d, filtered %d", len(r.Hits), r.Searched, r.Filtered)
	}
	r = searchWith(t, db, "otter", SearchOptions{Tags: []string{"wet", "Furry"}})
	if got := addresses(r); !reflect.DeepEqual(got, []string{strongAddr}) || r.Filtered != 1 {
		t.Errorf("two tags: %v, filtered %d", got, r.Filtered)
	}
	if got := r.Hits[0].Tags; !reflect.DeepEqual(got, []string{"furry", "wet"}) {
		t.Errorf("hit tags %v", got)
	}
}

func TestATagWordIsNotInTheTextIndex(t *testing.T) {
	db := rankDB(t)
	db.Tag(context.Background(), otherAddr, "marsupial")
	if r := search(t, db, "marsupial"); len(r.Hits) != 0 {
		t.Errorf("a tag matched as text: %v", addresses(r))
	}
}

func TestATagOnlyListingReturnsExactlyThePagesCarryingEveryTag(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Tag(ctx, weakAddr, "x", "y")
	db.Tag(ctx, strongAddr, "x")
	db.Tag(ctx, otherAddr, "y")
	r := searchWith(t, db, "", SearchOptions{Tags: []string{"x"}})
	// ordered by title then address: "Otter otter" < "Rivers"
	if got := addresses(r); !reflect.DeepEqual(got, []string{strongAddr, weakAddr}) {
		t.Errorf("x: %v", got)
	}
	r = searchWith(t, db, "", SearchOptions{Tags: []string{"x", "y"}})
	if got := addresses(r); !reflect.DeepEqual(got, []string{weakAddr}) {
		t.Errorf("x and y: %v", got)
	}
}

func TestATagOnlyListingDoesNotListAGonePage(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	root := rankSite(t)
	fill(t, db, root)
	db.Tag(ctx, weakAddr, "x")
	writeOutline(t, root, `<outline text="s" url="https://example.org/strong.md"/>`) // weak and other go
	fill(t, db, root)
	if r := searchWith(t, db, "", SearchOptions{Tags: []string{"x"}}); len(r.Hits) != 0 {
		t.Errorf("listed %v", addresses(r))
	}
}

func TestPinnedOnlyWithAQueryKeepsPinnedMatchesAndWithoutOneListsPinnedPages(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Pin(ctx, weakAddr, otherAddr)
	r := searchWith(t, db, "otter", SearchOptions{Pinned: true})
	if got := addresses(r); !reflect.DeepEqual(got, []string{weakAddr}) || r.Filtered != 2 {
		t.Errorf("with query: %v, filtered %d", got, r.Filtered)
	}
	r = searchWith(t, db, "", SearchOptions{Pinned: true})
	// by title: Badgers, Rivers
	if got := addresses(r); !reflect.DeepEqual(got, []string{otherAddr, weakAddr}) {
		t.Errorf("no query: %v", got)
	}
	db.Tag(ctx, weakAddr, "x")
	r = searchWith(t, db, "", SearchOptions{Pinned: true, Tags: []string{"x"}})
	if got := addresses(r); !reflect.DeepEqual(got, []string{weakAddr}) {
		t.Errorf("pinned and tag: %v", got)
	}
}

func TestSearchWithNoQueryAndNoFilterFindsNothing(t *testing.T) {
	db := rankDB(t)
	if r := searchWith(t, db, "", SearchOptions{}); len(r.Hits) != 0 {
		t.Errorf("hits %v", addresses(r))
	}
}

func TestSearchStaysAndMatchesSearchWith(t *testing.T) {
	db := rankDB(t)
	db.Pin(context.Background(), weakAddr)
	a := addresses(search(t, db, "otter"))
	b := addresses(searchWith(t, db, "otter", SearchOptions{Limit: 20}))
	if !reflect.DeepEqual(a, b) {
		t.Errorf("%v vs %v", a, b)
	}
}

func TestAVersion3FileUpgradesInPlaceAndKeepsEverything(t *testing.T) {
	db, path := open(t)
	ctx := context.Background()
	fill(t, db, rankSite(t))
	db.Exclude(ctx, "https://example.org/zzz/")
	db.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TABLE IF EXISTS pins`, `DROP TABLE IF EXISTS tags`, `UPDATE meta SET value = '3' WHERE key = 'schema_version'`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open v3: %v", err)
	}
	defer db2.Close()
	if len(search(t, db2, "otter").Hits) != 2 {
		t.Error("pages lost")
	}
	if ex, _ := db2.Excluded(ctx); len(ex) != 1 {
		t.Errorf("exclusions %v", ex)
	}
	if _, err := db2.Pin(ctx, weakAddr); err != nil {
		t.Errorf("pin after upgrade: %v", err)
	}
	if _, err := db2.Tag(ctx, weakAddr, "x"); err != nil {
		t.Errorf("tag after upgrade: %v", err)
	}
	var v string
	db2.sql.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if v != "6" || SchemaVersion != 6 {
		t.Errorf("schema_version %q, constant %d", v, SchemaVersion)
	}
}

// A program at version 3 refused a newer file by comparing with its own
// constant; this checks the same rule against the current constant (a file one
// version ahead), since the old constant no longer exists to build against.
func TestAFileOneVersionAheadIsRefused(t *testing.T) {
	db, path := open(t)
	db.Close()
	raw, _ := sql.Open("sqlite", path)
	raw.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(SchemaVersion+1))
	raw.Close()
	if _, err := Open(path); !errors.Is(err, ErrSchema) {
		t.Errorf("err %v", err)
	}
}

func TestStatsCountsPinsAndTags(t *testing.T) {
	db := rankDB(t)
	ctx := context.Background()
	db.Pin(ctx, weakAddr, otherAddr)
	db.Tag(ctx, weakAddr, "x", "y")
	db.Tag(ctx, strongAddr, "x")
	st, err := db.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Pinned != 2 || st.Tags != 2 || st.TagPages != 3 {
		t.Errorf("stats %+v", st)
	}
}
