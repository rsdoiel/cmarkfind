package corpus

// Index sets: one set per site the corpus holds pages for (a site is one
// instance of a Markdown web site: an outline address, a folder, later a
// collection). A set owns an index folder; `search` reads every set that is not
// ignored. There is no corpus-wide index.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rsdoiel/cmarkfind/index"
)

var (
	// ErrNoSet says no set has the name given (exit class no_input, 66).
	ErrNoSet = errors.New("corpus: no such set")
	// ErrAmbiguousSet says a name fits more than one set (exit class usage, 2).
	ErrAmbiguousSet = errors.New("corpus: the name fits more than one set")
	// ErrAllIgnored says there are sets and every one is ignored (exit class
	// negative, 1).
	ErrAllIgnored = errors.New("corpus: every set is ignored")
)

/** SetInfo describes one index set.
 *
 * Fields:
 *   Name (string)         — the site key: an outline address or a folder
 *   Source (string)       — where the set's index comes from; "local" is built here from the pages
 *   Pages (int)           — live pages the set holds
 *   Ignored (bool)        — left out of an aggregate search
 *   Fields (string)       — the field mapping blob; "" is the built-in default
 *   FieldsVersion (int)   — the blob's version
 *   IndexDir (string)     — the folder the set's index is kept in
 *   Built (bool)          — whether the folder holds an index
 *   Root (string)         — the folder a collection was filled from; empty for any other set
 *
 * Example:
 *   for _, s := range sets { fmt.Println(s.Name, s.Pages, s.Ignored) }
 */
type SetInfo struct {
	Name          string
	Source        string
	Pages         int
	Ignored       bool
	Fields        string
	FieldsVersion int
	IndexDir      string
	Built         bool
	Root          string // the folder of a collection; empty for any other set
}

// ensureSets makes the sets table agree with the pages: a set for every site
// that has a page row, none for a site that has none.
func (d *DB) ensureSets(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO sets(site, source, added_at) SELECT DISTINCT site, 'local', ? FROM pages WHERE site <> ''`, stamp()); err != nil {
		return classify(err)
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT site FROM sets WHERE source = 'local' AND site NOT IN (SELECT DISTINCT site FROM pages)`)
	if err != nil {
		return classify(err)
	}
	var stale []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return classify(err)
		}
		stale = append(stale, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return classify(err)
	}
	return d.dropSets(ctx, stale) // a site whose pages are all deleted has no set, and no index folder
}

// slug is a readable, folder-safe form of a site key.
func slug(site string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(site) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[len(s)-40:], "-")
	}
	if s == "" {
		s = "set"
	}
	return s
}

/** SetIndexDir returns the folder the index of a set is kept in:
 * IndexDir/sets/SLUG-HASH, where the hash is the first eight hex digits of the
 * SHA-256 of the site key, so two sites that slug alike never share a folder.
 *
 * Parameters:
 *   site (string) — the set's site key
 *
 * Returns:
 *   string — the folder
 *
 * Example:
 *   dir := db.SetIndexDir("https://example.org/.well-known/toc.opml")
 */
func (d *DB) SetIndexDir(site string) string {
	sum := sha256.Sum256([]byte(site))
	return filepath.Join(d.IndexDir(), "sets", slug(site)+"-"+hex.EncodeToString(sum[:])[:8])
}

/** Sets lists the index sets, by name.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *
 * Returns:
 *   []SetInfo — every set; empty for a corpus with no pages
 *   error     — from the database
 *
 * Example:
 *   sets, err := db.Sets(ctx)
 */
func (d *DB) Sets(ctx context.Context) ([]SetInfo, error) {
	if err := d.ensureSets(ctx); err != nil {
		return nil, err
	}
	rows, err := d.sql.QueryContext(ctx, `
SELECT s.site, s.source, s.ignored, s.fields_version, s.fields,
       (SELECT count(*) FROM pages p WHERE p.site = s.site AND p.gone_at = ''),
       coalesce((SELECT root FROM collections c WHERE c.name = s.site), '')
FROM sets s ORDER BY s.site`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []SetInfo{}
	for rows.Next() {
		var s SetInfo
		var ign int
		if err := rows.Scan(&s.Name, &s.Source, &ign, &s.FieldsVersion, &s.Fields, &s.Pages, &s.Root); err != nil {
			return nil, classify(err)
		}
		s.Ignored = ign != 0
		s.IndexDir = d.SetIndexDir(s.Name)
		_, ferr := os.Stat(filepath.Join(s.IndexDir, "manifest.json"))
		s.Built = ferr == nil
		if s.Source != "local" && s.Built { // a published index holds the pages, not the corpus
			if ix, err := index.Open(s.IndexDir); err == nil {
				s.Pages = ix.Count()
			}
		}
		out = append(out, s)
	}
	return out, classify(rows.Err())
}

// plainName takes the scheme off a name when it has none to match against, and
// the outline path and trailing slash off any site address.
func plainName(s string) (scheme, rest string) {
	rest = s
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(rest, p) {
			scheme, rest = p, rest[len(p):]
			break
		}
	}
	rest = strings.TrimSuffix(rest, "/.well-known/toc.opml")
	return scheme, strings.TrimRight(rest, "/")
}

/** ResolveSet finds the set a name means. The name may be the site key, a site
 * address with or without the scheme and with or without the trailing slash or
 * the outline path (`rsdoiel.github.io` finds
 * `http://rsdoiel.github.io/.well-known/toc.opml`), or a folder.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   name (string)         — what the person typed
 *
 * Returns:
 *   string — the set's site key
 *   error  — ErrNoSet when nothing fits; ErrAmbiguousSet, naming each candidate,
 *            when more than one does
 *
 * Example:
 *   site, err := db.ResolveSet(ctx, "rsdoiel.github.io")
 */
func (d *DB) ResolveSet(ctx context.Context, name string) (string, error) {
	sets, err := d.Sets(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range sets {
		if s.Name == name {
			return s.Name, nil
		}
	}
	scheme, rest := plainName(name)
	if scheme == "" {
		if abs, err := filepath.Abs(name); err == nil {
			for _, s := range sets {
				if s.Name == abs {
					return s.Name, nil
				}
			}
		}
	}
	var hit []string
	for _, s := range sets {
		sc, r := plainName(s.Name)
		if r == rest && (scheme == "" || sc == scheme) {
			hit = append(hit, s.Name)
		}
	}
	switch len(hit) {
	case 0:
		return "", fmt.Errorf("%w: %s", ErrNoSet, name)
	case 1:
		return hit[0], nil
	}
	return "", fmt.Errorf("%w: %s fits %s", ErrAmbiguousSet, name, strings.Join(hit, " and "))
}

/** IgnoreSet marks a set ignored (left out of an aggregate search) or not.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the update
 *   site (string)         — the set's site key, as ResolveSet returns it
 *   ignored (bool)        — the new state
 *
 * Returns:
 *   error — ErrNoSet when there is no such set; database errors
 *
 * Example:
 *   err := db.IgnoreSet(ctx, site, true)
 */
func (d *DB) IgnoreSet(ctx context.Context, site string, ignored bool) error {
	if err := d.ensureSets(ctx); err != nil {
		return err
	}
	v := 0
	if ignored {
		v = 1
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE sets SET ignored = ? WHERE site = ?`, v, site)
	n, err := affected(res, err)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	return nil
}

/** SetFields stores a set's field mapping: an opaque blob and its version. The
 * program never reads the blob; a change of either value makes the set's index
 * out of date, so the next build rebuilds it. An empty blob is the built-in
 * default.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the update
 *   site (string)         — the set's site key
 *   version (int)         — the blob's format version
 *   blob (string)         — the mapping; "" clears it
 *
 * Returns:
 *   error — ErrNoSet when there is no such set; database errors
 *
 * Example:
 *   err := db.SetFields(ctx, site, 1, `{"author":["facet"]}`)
 */
func (d *DB) SetFields(ctx context.Context, site string, version int, blob string) error {
	if err := d.ensureSets(ctx); err != nil {
		return err
	}
	if err := d.refuseRemoteSet(ctx, site); err != nil {
		return err
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE sets SET fields_version = ?, fields = ? WHERE site = ?`, version, blob, site)
	n, err := affected(res, err)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	return nil
}

// dropSets removes each set's row and index folder.
func (d *DB) dropSets(ctx context.Context, sites []string) error {
	for _, s := range sites {
		if err := os.RemoveAll(d.SetIndexDir(s)); err != nil {
			return fmt.Errorf("%w: %w", ErrIndex, err)
		}
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM sets WHERE site = ?`, s); err != nil {
			return classify(err)
		}
	}
	return nil
}

// retireTopLevel removes the index files an older cmarkfind kept at the top of
// the index folder (one index for the whole corpus). The owner file and the
// sets folder stay.
func (d *DB) retireTopLevel() error {
	dir := d.IndexDir()
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIndex, err)
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == ownerFile {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("%w: %w", ErrIndex, err)
		}
	}
	return nil
}

/** SetBuild is what BuildSets did.
 *
 * Fields:
 *   Built (int)     — sets whose index was written
 *   Unchanged (int) — sets whose index was already current
 *
 * Example:
 *   fmt.Printf("built %d, unchanged %d\n", b.Built, b.Unchanged)
 */
type SetBuild struct {
	Built     int
	Unchanged int
}

type setPage struct{ address, title, description, body, hash, meta string }

// setPages are a set's live pages that are not excluded, in address order.
func (d *DB) setPages(ctx context.Context, site string, skip func(string) bool) ([]setPage, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT address, title, description, body, hash, meta FROM pages WHERE site = ? AND gone_at = '' ORDER BY address`, site)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []setPage
	for rows.Next() {
		var p setPage
		if err := rows.Scan(&p.address, &p.title, &p.description, &p.body, &p.hash, &p.meta); err != nil {
			return nil, classify(err)
		}
		if !skip(p.address) {
			out = append(out, p)
		}
	}
	return out, classify(rows.Err())
}

// buildRevision changes when what an index is built from the same pages changes
// (2: filter files for the front matter fields), so every set is built again once.
const buildRevision = 2

// inputDigest is what a set's index is a function of: the analyzer, the field
// mapping and the pages that go in.
func inputDigest(fieldsVersion int, fields string, pages []setPage) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%s\n%d\n", index.Analyzer, fieldsVersion, fields, buildRevision)
	for _, p := range pages {
		m := sha256.Sum256([]byte(p.meta))
		fmt.Fprintf(h, "%s %s %x\n", p.address, p.hash, m[:6])
	}
	return hex.EncodeToString(h.Sum(nil))
}

/** BuildSets builds the index of a set, or of every set when site is empty, but
 * only the ones that need it: a set is built when its folder holds no index, the
 * index is not the one the corpus last built, or its inputs (the analyzer, the
 * field mapping, the pages it holds after exclusions) differ from the last
 * build's. The generation goes up by one from the index already there, and the
 * files the old manifest named are pruned.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the build
 *   site (string)         — the set's site key as ResolveSet returns it; "" for every set
 *   chunk (int)           — entries per word chunk; 0 means the default
 *
 * Returns:
 *   *SetBuild — how many sets were built and how many were already current
 *   error     — ErrNoSet; database errors; index.ErrData for a bad page set;
 *               ErrIndex when files cannot be written; ErrIndexShared when
 *               another corpus owns the folder
 *
 * Example:
 *   b, err := db.BuildSets(ctx, "", 0)
 */
func (d *DB) BuildSets(ctx context.Context, site string, chunk int) (*SetBuild, error) {
	if err := d.claim(ctx); err != nil {
		return nil, err
	}
	sets, err := d.Sets(ctx)
	if err != nil {
		return nil, err
	}
	var todo []string
	for _, s := range sets {
		if (site == "" || s.Name == site) && s.Source == "local" { // a mirror or reference is never built here
			todo = append(todo, s.Name)
		}
	}
	if site != "" && len(todo) == 0 && !hasSet(sets, site) {
		return nil, fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	out := &SetBuild{}
	for _, s := range todo {
		_, built, err := d.buildSet(ctx, s, chunk, false)
		if err != nil {
			return out, err
		}
		if built {
			out.Built++
		} else {
			out.Unchanged++
		}
	}
	return out, nil
}

// buildSet builds one set when it needs it (or when force is true) and returns
// its opened index and whether it was written.
func (d *DB) buildSet(ctx context.Context, site string, chunk int, force bool) (*index.Index, bool, error) {
	var version, gen int
	var fields, stored, oldOnly, source string
	err := d.sql.QueryRowContext(ctx, `SELECT fields_version, fields, input_digest, generation, document_only, source FROM sets WHERE site = ?`, site).
		Scan(&version, &fields, &stored, &gen, &oldOnly, &source)
	if err != nil {
		return nil, false, classify(err)
	}
	if source != "local" {
		return nil, false, fmt.Errorf("%w: %s", ErrRemoteSet, site) // its index is the publisher's, not built here
	}
	skip, err := d.excludedBy(ctx)
	if err != nil {
		return nil, false, err
	}
	pages, err := d.setPages(ctx, site, skip)
	if err != nil {
		return nil, false, err
	}
	digest := inputDigest(version, fields, pages)
	dir := d.SetIndexDir(site)
	old, oerr := index.Open(dir)
	if !force && oerr == nil && digest == stored && int(old.Generation()) == gen {
		return old, false, nil
	}
	mapping, err := d.FieldMapping(ctx, site)
	if err != nil {
		return nil, false, err
	}
	docs := make([]index.Doc, len(pages))
	for i, p := range pages {
		fields, text := mapping.pageFields(p.meta)
		desc := p.description
		if len(text) > 0 { // the text role: the field's words are searched with the description
			desc = strings.TrimSpace(desc + " " + strings.Join(text, " "))
		}
		docs[i] = index.Doc{Address: p.address, Title: p.title, Description: desc, Body: p.body, Fields: fields}
	}
	next := uint64(1)
	if oerr == nil {
		next = old.Generation() + 1
	}
	rep, err := index.BuildWithReport(dir, docs, index.BuildOptions{ChunkEntries: chunk, Generation: next, Fields: mapping.specs()})
	if err != nil {
		if index.IsDataError(err) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: %w", ErrIndex, err)
	}
	if _, err := index.Prune(dir); err != nil {
		return nil, false, err
	}
	ix, err := index.Open(dir)
	if err != nil {
		return nil, false, err
	}
	d.mu.Lock()
	d.report = rep
	d.mu.Unlock()
	words := ix.DocumentOnly()
	var skipped []string
	for _, s := range rep.Skipped {
		skipped = append(skipped, s.Address)
	}
	sort.Strings(skipped)
	_, err = d.sql.ExecContext(ctx, `UPDATE sets SET input_digest = ?, generation = ?, document_only = ?, not_indexed = ? WHERE site = ?`,
		digest, ix.Generation(), strings.Join(words, "\n"), strings.Join(skipped, "\n"), site)
	if err != nil {
		return nil, false, classify(err)
	}
	d.noteBuild(rep, words, oldOnly)
	d.noteDates(ctx, site, pages)
	return ix, true, nil
}

// noteBuild queues the warnings a build raised: the words now indexed without
// positions when that list differs from the set's previous build's (and is not
// empty), and the build's own warnings.
func (d *DB) noteBuild(rep *index.BuildReport, words []string, previous string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(words) > 0 && strings.Join(words, "\n") != previous {
		shown, more := words, ""
		if len(shown) > 8 {
			shown, more = shown[:8], fmt.Sprintf(" and %d more", len(words)-8)
		}
		d.warnings = append(d.warnings, fmt.Sprintf("%d %s now indexed without positions: %s%s (a phrase through %s is checked against each page's text)",
			len(words), pluralWords(len(words)), strings.Join(shown, ", "), more, pronoun(len(words), "it", "them")))
	}
	if rep != nil {
		d.warnings = append(d.warnings, rep.Warnings...)
	}
}

// currentSet opens a set's index, building it first when the folder holds none
// or the one there is not the one this corpus last built.
func (d *DB) currentSet(ctx context.Context, site string) (*index.Index, error) {
	if err := d.claim(ctx); err != nil {
		return nil, err
	}
	var gen int
	var source string
	err := d.sql.QueryRowContext(ctx, `SELECT generation, source FROM sets WHERE site = ?`, site).Scan(&gen, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	if err != nil {
		return nil, classify(err)
	}
	if source != "local" {
		return d.openRemoteSet(ctx, site, source)
	}
	ix, oerr := index.Open(d.SetIndexDir(site))
	if oerr == nil && int(ix.Generation()) == gen && gen > 0 {
		return ix, nil
	}
	if index.IsAnalyzerError(oerr) && source != "local" {
		// an index fetched from elsewhere in a word rule this program lacks is
		// not ours to rebuild
		return nil, fmt.Errorf("%s: %w", site, oerr)
	}
	ix, _, err = d.buildSet(ctx, site, 0, true)
	return ix, err
}

// setsToSearch is the sets a search reads: the one named, or every set that is
// not ignored. ErrAllIgnored when there are sets and each is ignored.
func (d *DB) setsToSearch(ctx context.Context, one string) ([]string, error) {
	sets, err := d.Sets(ctx)
	if err != nil {
		return nil, err
	}
	if one != "" {
		for _, s := range sets {
			if s.Name == one {
				return []string{one}, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrNoSet, one)
	}
	var out []string
	for _, s := range sets {
		if !s.Ignored {
			out = append(out, s.Name)
		}
	}
	if len(sets) > 0 && len(out) == 0 {
		return nil, ErrAllIgnored
	}
	return out, nil
}

// inList is "(?,?,?)" and its arguments, for a site list.
func inList(sites []string) (string, []any) {
	args := make([]any, len(sites))
	for i, s := range sites {
		args[i] = s
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(sites)), ",") + ")", args
}

// noteDates queues a warning for each date a set's date fields hold that is not
// a date, naming the page, the field and the text, and a count line. The page is
// left with no value for that field and is still indexed by its text.
func (d *DB) noteDates(ctx context.Context, site string, pages []setPage) {
	m, err := d.FieldMapping(ctx, site)
	if err != nil {
		return
	}
	fields := m.DateFields()
	var lines []string
	for _, p := range pages {
		if p.meta == "" || p.meta == "{}" {
			continue
		}
		var fm map[string]any
		if json.Unmarshal([]byte(p.meta), &fm) != nil {
			continue
		}
		lower := map[string]any{}
		for k, v := range fm {
			lower[strings.ToLower(k)] = v
		}
		for _, f := range fields {
			v, ok := lower[f]
			if !ok {
				continue
			}
			text := ""
			switch x := v.(type) {
			case string:
				text = x
			case []any:
				if len(x) > 0 {
					text, _ = x[0].(string)
				}
			}
			if text == "" {
				continue
			}
			if _, _, ok := index.ParseDay(text); !ok {
				lines = append(lines, fmt.Sprintf("%s: field %s is not a date: %q", p.address, f, text))
			}
		}
	}
	if len(lines) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.warnings = append(d.warnings, lines...)
	d.warnings = append(d.warnings, fmt.Sprintf("%d %s could not be read and %s left without a value", len(lines), plural1(len(lines), "date", "dates"), plural1(len(lines), "is", "are")))
}

func plural1(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// hasSet reports whether sets holds one of that name.
func hasSet(sets []SetInfo, name string) bool {
	for _, s := range sets {
		if s.Name == name {
			return true
		}
	}
	return false
}
