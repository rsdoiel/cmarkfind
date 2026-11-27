package index

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

/** Built returns the time the manifest records for the build.
 *
 * Returns:
 *   string — RFC 3339, as written by Build
 *
 * Example:
 *   fmt.Println(ix.Built())
 */
func (ix *Index) Built() string { return ix.m.Built }

// tempPrefix starts the name of a file being written (see replaceFile); one
// left behind by a build that died is removed like any other leftover.
const tempPrefix = ".tmp-"

// DefaultPruneGrace is how long Prune leaves a file the manifest no longer
// names: a search that opened the previous generation reads its files lazily,
// and a rebuild must not take them away from under it.
const DefaultPruneGrace = 10 * time.Minute

/** Prune removes the index files in dir (the w, d and f folders) that the
 * current manifest does not name and that were last written more than
 * DefaultPruneGrace ago: what earlier builds left behind, once no search that
 * began before the rebuild can still be reading it. Only *.cbor and *.cbor.gz
 * files in those folders are touched. The manifest must open and its document
 * tables must read; otherwise nothing is removed.
 *
 * Parameters:
 *   dir (string) — the index directory
 *
 * Returns:
 *   int   — files removed
 *   error — ErrMissing without a manifest; ErrData when a table is wrong
 *
 * Example:
 *   n, err := index.Prune("idx")
 */
func Prune(dir string) (int, error) { return PruneOlderThan(dir, DefaultPruneGrace) }

/** PruneOlderThan is Prune with the grace period given; 0 removes every file
 * the manifest does not name, at once.
 *
 * Parameters:
 *   dir (string)          — the index directory
 *   grace (time.Duration) — files modified more recently than this are kept
 *
 * Returns:
 *   int   — files removed
 *   error — as Prune
 *
 * Example:
 *   n, err := index.PruneOlderThan("idx", 0)
 */
func PruneOlderThan(dir string, grace time.Duration) (int, error) {
	ix, err := Open(dir)
	if err != nil {
		return 0, err
	}
	rows, err := ix.docRows()
	if err != nil {
		return 0, err
	}
	live := map[string]bool{}
	for _, c := range ix.m.Chunks {
		live[compressedName("w", c.File.Digest, ix.m.Compression)] = true
	}
	for _, r := range ix.m.Documents.Ranges {
		live[compressedName("d", r.File.Digest, ix.m.Compression)] = true
	}
	for _, r := range rows {
		live[compressedName("f", hex.EncodeToString(r.Fragment), ix.m.Compression)] = true
	}
	removed := 0
	for _, kind := range []string{".", "w", "d", "f"} {
		ents, err := os.ReadDir(filepath.Join(dir, kind))
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := path.Join(kind, e.Name())
			if kind == "." {
				if !strings.HasPrefix(e.Name(), tempPrefix) {
					continue // the root holds the manifest and what is not ours
				}
				name = e.Name()
			}
			temp := strings.HasPrefix(e.Name(), tempPrefix)
			if e.IsDir() || (!temp && (live[name] || !(strings.HasSuffix(name, ".cbor") || strings.HasSuffix(name, ".cbor.gz")))) {
				continue
			}
			if grace > 0 {
				if info, err := e.Info(); err == nil && time.Since(info.ModTime()) < grace {
					continue
				}
			}
			err := os.Remove(filepath.Join(dir, filepath.FromSlash(name)))
			if errors.Is(err, fs.ErrNotExist) {
				continue // another prune got there first
			}
			if err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}
