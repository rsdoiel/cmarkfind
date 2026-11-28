// Package remote reads a partitioned index published on the web (cmarkfind
// DR-0006, DR-0007, Phase 7): a Source that is an index folder behind an
// address, and Mirror, which keeps a verified copy on disk.
//
// A manifest names every file by the digest of its bytes, never by an address,
// so the only addresses made here are base + "manifest.json" and
// base + "kind/digest.cbor[.gz]". Redirects may not leave the origin
// (fetch.Options.SameOrigin, forced on here), a body is capped, and a file is
// checked against its digest by the index reader before it is used.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/markdownweb/fetch"
)

// ErrRollback marks a published index whose generation is lower than the one
// held. It is a data error (exit 65).
var ErrRollback = fmt.Errorf("%w: the published generation is lower than the one held", index.ErrData)

// ErrNotAMirror marks a folder that holds files but no index, which Mirror will
// not replace (exit 73).
var ErrNotAMirror = errors.New("the folder holds files that are not an index")

// fileName is every name the format makes: the manifest, or kind/digest.cbor[.gz].
var fileName = regexp.MustCompile(`^(manifest\.json|[a-z]/[0-9a-f]{64}\.cbor(\.gz)?)$`)

/** Source is an index folder published at an address; it is an index.Source
 * (an fs.FS). Nothing is requested until a file is opened, and only the names
 * the format makes are ever turned into an address.
 *
 * Example:
 *   src, err := remote.NewSource(ctx, fetch.Options{UserAgent: "cmarkfind/0.1"}, "https://example.org/cmarkfind/manifest.json")
 *   ix, err := index.OpenSource(src)
 */
type Source struct {
	ctx  context.Context
	f    *fetch.Fetcher
	base *url.URL // the folder of the manifest, with a trailing slash
}

/** NewSource makes a Source for the index whose manifest is at manifestURL.
 *
 * Parameters:
 *   ctx (context.Context)  — cancels every request the Source makes
 *   opts (fetch.Options)   — the fetcher's options; SameOrigin is always on
 *   manifestURL (string)   — an http or https address ending in /manifest.json
 *
 * Returns:
 *   *Source — the source; no request has been made
 *   error   — a data error (index.ErrData) for an address that is not such a manifest address
 *
 * Example:
 *   src, err := remote.NewSource(ctx, fetch.Options{}, "https://example.org/cmarkfind/manifest.json")
 */
func NewSource(ctx context.Context, opts fetch.Options, manifestURL string) (*Source, error) {
	u, err := url.Parse(manifestURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil ||
		path.Base(u.Path) != "manifest.json" {
		return nil, fmt.Errorf("%w: %q is not the address of a manifest.json", index.ErrData, manifestURL)
	}
	u.Path = strings.TrimSuffix(u.Path, "manifest.json")
	u.RawPath = ""
	opts.SameOrigin = true
	return &Source{ctx: ctx, f: fetch.New(opts), base: u}, nil
}

/** Open is fs.FS: it fetches the file and returns it whole.
 *
 * Parameters:
 *   name (string) — "manifest.json" or "kind/digest.cbor[.gz]"; anything else is refused with fs.ErrInvalid and no request
 *
 * Returns:
 *   fs.File — the file's bytes
 *   error   — fs.ErrNotExist for a 404; a wrapped fetch.ErrUnavailable when the server cannot be reached or answers badly; a data error (index.ErrData) for a body over the cap or a redirect to another origin
 *
 * Example:
 *   f, err := src.Open("manifest.json")
 */
func (s *Source) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) || !fileName.MatchString(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	href := s.base.String() + name
	res, err := s.f.Get(s.ctx, href, "")
	if err != nil {
		return nil, classify(err)
	}
	switch {
	case res.Status == 404 || res.Status == 410:
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	case res.Status < 200 || res.Status > 299:
		return nil, fmt.Errorf("%w: %s answered HTTP %d", fetch.ErrUnavailable, href, res.Status)
	}
	return &file{name: name, r: bytes.NewReader(res.Body), size: int64(len(res.Body))}, nil
}

// classify makes a fetch error one the index package's classes recognise: a
// body too large or a cross-origin redirect is wrong data, whatever the
// transport called it. Unavailable stays as it is.
func classify(err error) error {
	if errors.Is(err, fetch.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, fetch.ErrData) && !errors.Is(err, index.ErrData) {
		return fmt.Errorf("%w: %v", index.ErrData, err)
	}
	return err
}

// file is a fetched file as an fs.File.
type file struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *file) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *file) Close() error               { return nil }
func (f *file) Stat() (fs.FileInfo, error) { return info{f.name, f.size}, nil }

type info struct {
	name string
	size int64
}

func (i info) Name() string       { return path.Base(i.name) }
func (i info) Size() int64        { return i.size }
func (i info) Mode() fs.FileMode  { return 0o444 }
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return false }
func (i info) Sys() any           { return nil }

// recorder is a Source that keeps what it reads: each file is fetched once,
// written under dir at its own name, and answered from memory after that.
type recorder struct {
	src   *Source
	dir   string
	cache map[string][]byte
	n     int
}

func (r *recorder) Open(name string) (fs.File, error) {
	if b, ok := r.cache[name]; ok {
		return &file{name: name, r: bytes.NewReader(b), size: int64(len(b))}, nil
	}
	f, err := r.src.Open(name)
	if err != nil {
		return nil, err
	}
	fl := f.(*file)
	b := make([]byte, fl.size)
	if _, err := fl.r.Read(b); err != nil && fl.size > 0 {
		return nil, err
	}
	dst := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return nil, err
	}
	r.cache[name] = b
	r.n++
	return &file{name: name, r: bytes.NewReader(b), size: int64(len(b))}, nil
}

/** Report says what a Mirror did.
 *
 * Fields:
 *   Generation (uint64) — the generation the mirror holds now
 *   Changed (bool)      — the mirror was made or replaced; false when it was already current
 *   Files (int)         — files fetched and stored (the manifest included); 0 when unchanged
 */
type Report struct {
	Generation uint64
	Changed    bool
	Files      int
}

/** Mirror keeps a verified copy of the index at manifestURL in dir.
 *
 * It reads the manifest alone first. A generation lower than the copy held is
 * ErrRollback and nothing changes; an equal one changes nothing and fetches no
 * shard; a higher one (or no copy yet) fetches every file into a staging folder
 * beside dir, verifies it, and swaps it in whole, so dir is always a complete
 * index. A failure leaves dir as it was and no staging folder behind. dir must
 * not exist, be empty, or hold an index of this kind: other files are never
 * replaced (ErrNotAMirror).
 *
 * Parameters:
 *   ctx (context.Context) — cancels the fetches
 *   opts (fetch.Options)  — the fetcher's options; SameOrigin is always on
 *   manifestURL (string)  — the address of the manifest.json
 *   dir (string)          — the folder that holds the copy
 *
 * Returns:
 *   *Report — what was done
 *   error   — ErrRollback and other data errors (index.IsDataError), index.IsMissing, a wrapped fetch.ErrUnavailable, ErrNotAMirror, or an I/O error
 *
 * Example:
 *   rep, err := remote.Mirror(ctx, fetch.Options{}, "https://example.org/cmarkfind/manifest.json", "idx/example")
 */
func Mirror(ctx context.Context, opts fetch.Options, manifestURL, dir string) (*Report, error) {
	dir = filepath.Clean(dir)
	held, err := heldGeneration(dir)
	if err != nil {
		return nil, err
	}
	src, err := NewSource(ctx, opts, manifestURL)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".stage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	rec := &recorder{src: src, dir: stage, cache: map[string][]byte{}}

	ix, err := index.OpenSource(rec) // the manifest, checked
	if err != nil {
		return nil, err
	}
	gen := ix.Generation()
	if held != nil {
		switch {
		case gen < *held:
			return nil, fmt.Errorf("%w (published %d, held %d)", ErrRollback, gen, *held)
		case gen == *held:
			return &Report{Generation: gen}, nil
		}
	}
	if err := index.VerifySource(rec); err != nil { // every file, against its digest
		return nil, err
	}
	if err := index.Verify(stage); err != nil { // and again from what was stored
		return nil, err
	}
	if err := swap(stage, dir); err != nil {
		return nil, err
	}
	return &Report{Generation: gen, Changed: true, Files: rec.n}, nil
}

// heldGeneration looks at dir before anything is fetched: nil for a folder that
// is absent or empty or holds a damaged index of ours (to be replaced), the
// generation of a sound one, ErrNotAMirror for a folder of someone's files.
func heldGeneration(dir string) (*uint64, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(ents) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotAMirror, dir)
	}
	ix, err := index.Open(dir)
	if err != nil {
		if index.IsDataError(err) {
			return nil, nil // ours, and damaged: the fetch replaces it
		}
		return nil, err
	}
	g := ix.Generation()
	return &g, nil
}

// swap puts the staged index in place of dir, whole: the old one is moved
// aside first and put back if the new one cannot be moved in.
func swap(stage, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(stage, dir)
	}
	old := dir + ".old-" + fmt.Sprint(time.Now().UnixNano())
	if err := os.Rename(dir, old); err != nil {
		return err
	}
	if err := os.Rename(stage, dir); err != nil {
		os.Rename(old, dir)
		return err
	}
	os.RemoveAll(old)
	return nil
}
