package cli

import (
	"database/sql"

	_ "github.com/glebarez/go-sqlite"
)

// setSchema writes a schema_version into an existing corpus, to test a newer one.
func setSchema(path, v string) error {
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer raw.Close()
	_, err = raw.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, v)
	return err
}

func openRaw(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }
