// Package remote reads a partitioned index published on the web (Phase 7):
// a Source that is an index directory behind an address, and Mirror, which
// keeps a verified copy on disk.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/markdownweb/fetch"
)

// ErrRollback marks a published index whose generation is lower than the one
// held. It is a data error (exit 65).
var ErrRollback = fmt.Errorf("%w: the published generation is lower than the one held", index.ErrData)

var errNotBuilt = errors.New("remote: not implemented")

// Source is an index directory published at an address; it is an index.Source.
type Source struct{}

/** NewSource makes a Source for the index whose manifest is at manifestURL.
 * It is a stub until Phase 7 is built.
 */
func NewSource(ctx context.Context, f *fetch.Fetcher, manifestURL string) (*Source, error) {
	return nil, errNotBuilt
}

// Open is fs.FS.
func (s *Source) Open(name string) (fs.File, error) { return nil, errNotBuilt }

// Report says what a Mirror did.
type Report struct {
	Generation uint64
	Changed    bool
	Files      int
}

/** Mirror keeps a verified copy of the index at manifestURL in dir. It is a
 * stub until Phase 7 is built.
 */
func Mirror(ctx context.Context, f *fetch.Fetcher, manifestURL, dir string) (*Report, error) {
	return nil, errNotBuilt
}
