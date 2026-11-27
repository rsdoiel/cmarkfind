package index

import (
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"strings"
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

/** Prune removes the index files in dir (the w, d and f folders) that the
 * current manifest does not name: what earlier builds left behind. Only
 * *.cbor and *.cbor.gz files in those folders are touched. The manifest must
 * open and its document tables must read; otherwise nothing is removed.
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
func Prune(dir string) (int, error) {
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
	for _, kind := range []string{"w", "d", "f"} {
		ents, err := os.ReadDir(filepath.Join(dir, kind))
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := path.Join(kind, e.Name())
			if e.IsDir() || live[name] || !(strings.HasSuffix(name, ".cbor") || strings.HasSuffix(name, ".cbor.gz")) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}
