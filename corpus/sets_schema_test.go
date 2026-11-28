package corpus

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// A schema 5 file has no sets table (and no meta column). Opening it brings it to schema 7 in place:
// every page, pin, tag, exclusion and site row is kept, and the sets table is
// there for the next fill to use.
func TestAVersion5FileUpgradesInPlaceToSchema6KeepingEveryRow(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db, path := open(t)
	ctx := context.Background()
	fill(t, db, site(t, threePages))
	db.Pin(ctx, "https://example.org/a.md")
	db.Tag(ctx, "https://example.org/a.md", "x")
	db.Exclude(ctx, "https://example.org/zzz/")
	db.Close()

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE IF EXISTS sets; DROP TABLE IF EXISTS collections; UPDATE meta SET value = '5' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open schema 5: %v", err)
	}
	defer db2.Close()
	if SchemaVersion != 7 {
		t.Errorf("SchemaVersion = %d, want 7", SchemaVersion)
	}
	var v string
	db2.sql.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if v != strconv.Itoa(7) {
		t.Errorf("schema_version = %q, want 7", v)
	}
	tb := tables(t, path)
	for _, want := range []string{"sets", "collections"} {
		if !tb[want] {
			t.Errorf("no %s table after the upgrade", want)
		}
	}
	if n := search(t, db2, "badgers"); len(n.Hits) != 1 {
		t.Errorf("hits %v", addresses(n))
	}
	var pins, tags, ex int
	db2.sql.QueryRow(`SELECT count(*) FROM pins`).Scan(&pins)
	db2.sql.QueryRow(`SELECT count(*) FROM tags`).Scan(&tags)
	db2.sql.QueryRow(`SELECT count(*) FROM excluded`).Scan(&ex)
	if pins != 1 || tags != 1 || ex != 1 {
		t.Errorf("pins %d, tags %d, exclusions %d", pins, tags, ex)
	}
}

// A file written by a later schema than this program reads is refused (exit 65),
// so an installed cmarkbrowser built for schema 5 cannot damage a schema 6 file.
func TestAFileOfTheNextSchemaIsRefusedByThisOne(t *testing.T) {
	db, path := open(t)
	db.Close()
	raw, _ := sql.Open("sqlite", path)
	raw.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(SchemaVersion+1))
	raw.Close()
	if _, err := Open(path); err == nil {
		t.Error("a newer schema was opened")
	}
}
