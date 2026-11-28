package corpus

// Mirror and reference sets (cmarkfind DR-0017): a set whose index is a
// published one. A mirror keeps a verified copy of the index in the set's
// folder; a reference keeps only the manifest and fetches the rest at each
// search. Neither has pages in the corpus.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rsdoiel/cmarkfind/index"
	pubindex "github.com/rsdoiel/cmarkfind/remote"
	"github.com/rsdoiel/markdownweb/fetch"
)

var (
	// ErrRemoteSet marks an act that is for a set built here (a fill, a field
	// mapping, a build) done to a mirror or reference set.
	ErrRemoteSet = errors.New("corpus: the set's index is a published one")
	// ErrSetExists marks a name that is already a set (exit 73).
	ErrSetExists = errors.New("corpus: a set of that name exists")
)

// SourceFile is source.json, kept in a mirror's or reference's folder.
type sourceFile struct {
	Manifest   string `json:"manifest"`
	CheckedAt  string `json:"checked_at"`
	Generation uint64 `json:"generation"`
}

/** RemoteSet describes what an add or refresh did.
 *
 * Fields:
 *   Name (string)       — the set
 *   Source (string)     — "mirror" or "reference"
 *   Manifest (string)   — the address of its manifest.json
 *   Generation (uint64) — the generation held now
 *   Changed (bool)      — files were stored (always true for an add)
 *   Files (int)         — files stored
 */
type RemoteSet struct {
	Name       string
	Source     string
	Manifest   string
	Generation uint64
	Changed    bool
	Files      int
}

/** SetFetchOptions sets the options of the fetcher a reference, an add and a
 * refresh reach the web with (the User-Agent, mainly). Redirects stay on the
 * same origin whatever is set.
 *
 * Parameters:
 *   o (fetch.Options) — the options; the zero value is usable
 *
 * Example:
 *   db.SetFetchOptions(fetch.Options{UserAgent: "cmarkfind/0.1"})
 */
func (d *DB) SetFetchOptions(o fetch.Options) { d.fetchOpt = o }

/** SetSource returns where a set's index comes from.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   name (string)         — a set's name as Sets reports it
 *
 * Returns:
 *   string — "local", "mirror" or "reference"; empty when there is no such set
 *   error  — from the database
 *
 * Example:
 *   src, err := db.SetSource(ctx, "https://example.org/.well-known/toc.opml")
 */
func (d *DB) SetSource(ctx context.Context, name string) (string, error) {
	if err := d.ensureSets(ctx); err != nil {
		return "", err
	}
	var src string
	err := d.sql.QueryRowContext(ctx, `SELECT source FROM sets WHERE site = ?`, name).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return src, classify(err)
}

/** AddRemoteSet makes a set from a published index: a mirror (the files are
 * fetched, verified and stored) or a reference (the manifest is verified and
 * stored). Nothing is left behind when it fails.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fetches
 *   name (string)         — the set's name; ErrSetExists when a set has it
 *   source (string)       — "mirror" or "reference"
 *   manifestURL (string)  — the address of the manifest.json
 *
 * Returns:
 *   *RemoteSet — what was stored
 *   error      — ErrSetExists; ErrIndexShared; the errors of pubindex.Mirror (index.IsDataError, index.IsMissing, fetch.ErrUnavailable); database errors
 *
 * Example:
 *   rs, err := db.AddRemoteSet(ctx, "https://example.org/.well-known/toc.opml", "mirror", "https://example.org/cmarkfind/manifest.json")
 */
func (d *DB) AddRemoteSet(ctx context.Context, name, source, manifestURL string) (*RemoteSet, error) {
	if source != "mirror" && source != "reference" {
		return nil, fmt.Errorf("corpus: a set's source is mirror or reference, not %q", source)
	}
	if err := d.ensureSets(ctx); err != nil {
		return nil, err
	}
	var one int
	if err := d.sql.QueryRowContext(ctx, `SELECT 1 FROM sets WHERE site = ?`, name).Scan(&one); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrSetExists, name)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, classify(err)
	}
	if err := d.claim(ctx); err != nil {
		return nil, err
	}
	dir := d.SetIndexDir(name)
	if err := os.RemoveAll(dir); err != nil { // a folder left by an earlier run: ours, and no set owns it
		return nil, fmt.Errorf("%w: %w", ErrIndex, err)
	}
	var rep *pubindex.Report
	var err error
	if source == "mirror" {
		rep, err = pubindex.Mirror(ctx, d.fetchOpt, manifestURL, dir)
	} else {
		rep, err = pubindex.StoreManifest(ctx, d.fetchOpt, manifestURL, dir)
	}
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := writeSource(dir, sourceFile{Manifest: manifestURL, CheckedAt: stamp(), Generation: rep.Generation}); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if _, err := d.sql.ExecContext(ctx, `INSERT INTO sets(site, source, generation, added_at) VALUES (?, ?, ?, ?)`, name, source, rep.Generation, stamp()); err != nil {
		os.RemoveAll(dir)
		return nil, classify(err)
	}
	return &RemoteSet{Name: name, Source: source, Manifest: manifestURL, Generation: rep.Generation, Changed: true, Files: rep.Files}, nil
}

// writeSource writes source.json into dir, atomically.
func writeSource(dir string, sf sourceFile) error {
	b, _ := json.MarshalIndent(sf, "", "  ")
	tmp, err := os.CreateTemp(dir, ".source-")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("%w: %w", ErrIndex, errors.Join(werr, cerr))
	}
	os.Chmod(tmp.Name(), 0o644)
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "source.json")); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	return nil
}

// readSource reads source.json of a set's folder.
func readSource(dir string) (sourceFile, error) {
	var sf sourceFile
	b, err := os.ReadFile(filepath.Join(dir, "source.json"))
	if err != nil {
		return sf, err
	}
	if err := json.Unmarshal(b, &sf); err != nil || sf.Manifest == "" {
		return sf, fmt.Errorf("%w: source.json is not what this program writes", index.ErrData)
	}
	return sf, nil
}

// openRemoteSet opens the index of a mirror (from its folder) or a reference
// (its stored manifest, the rest fetched). A published index is never built here.
func (d *DB) openRemoteSet(ctx context.Context, site, source string) (*index.Index, error) {
	dir := d.SetIndexDir(site)
	if source == "mirror" {
		ix, err := index.Open(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", site, err)
		}
		return ix, nil
	}
	sf, err := readSource(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", site, err)
	}
	src, err := pubindex.NewReference(ctx, d.fetchOpt, sf.Manifest, dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", site, err)
	}
	ix, err := index.OpenSource(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", site, err)
	}
	return ix, nil
}

// publishedPages is the pages the mirror and reference sets among sites hold,
// by their manifests (a search counts them as searched).
func (d *DB) publishedPages(ctx context.Context, sites []string) (int, error) {
	sources, err := d.sourcesOf(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range sites {
		if src := sources[s]; src != "" && src != "local" {
			if ix, err := index.Open(d.SetIndexDir(s)); err == nil {
				n += ix.Count()
			}
		}
	}
	return n, nil
}

// refuseRemoteSet is ErrRemoteSet when name is a mirror or reference: an act
// for a set built here (a fill, a field mapping) is not for it.
func (d *DB) refuseRemoteSet(ctx context.Context, name string) error {
	src, err := d.SetSource(ctx, name)
	if err != nil {
		return err
	}
	if src != "" && src != "local" {
		return fmt.Errorf("%w: %s is a %s", ErrRemoteSet, name, src)
	}
	return nil
}
