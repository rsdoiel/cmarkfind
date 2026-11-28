package index

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/fxamacker/cbor/v2"
)

// Format names the index format of this package (format.cddl).
const Format = "cmarkfind/1"

const (
	maxFileSize     = 8 << 20
	maxManifestSize = 4 << 20
	maxFiles        = 1000000
)

const (
	// DefaultEntryBytes is the encoded size over which a word's entry is
	// written without positions (document-only): a quarter of the file limit.
	DefaultEntryBytes = 2 << 20
	// MaxTermBytes is the longest term indexed; a longer word (a hash, a
	// pasted blob) is left out of the index and still counted as a word.
	MaxTermBytes = 128
	// FeatureDocumentOnly is the manifest feature a reader must know to read an
	// index that has document-only words.
	FeatureDocumentOnly = "document-only-words/1"
)

// knownFeatures are the manifest "required" features this reader implements.
var knownFeatures = map[string]bool{FeatureDocumentOnly: true}

// Weights of the three parts of a page, in stream order.
const (
	weightTitle       = 4
	weightDescription = 2
	weightBody        = 1
)

var (
	// ErrData marks content that is wrong: a bad digest, a malformed file, an
	// unknown format. Commands exit 65 on it.
	ErrData = errors.New("index data is wrong")
	// ErrAnalyzer marks an index built with an analyzer (a word-splitting rule)
	// this reader does not implement. It is also a data error (65): such an
	// index cannot be searched here, though it may be sound.
	ErrAnalyzer = errors.New("index analyzer is not supported")
	// ErrMissing marks an index (or a file it needs) that cannot be found.
	// Commands exit 66 on it.
	ErrMissing = errors.New("index not found")
	// ErrTooLarge marks a build that would write a file over the file limit
	// even with its most common words document-only. Commands exit 73 on it.
	ErrTooLarge = errors.New("index file is too large")
)

/** IsDataError reports whether err is (or wraps) a data error: content read
 * from an index is wrong.
 *
 * Parameters:
 *   err (error) — any error
 *
 * Returns:
 *   bool — true for ErrData
 *
 * Example:
 *   if index.IsDataError(err) { os.Exit(65) }
 */
func IsDataError(err error) bool { return errors.Is(err, ErrData) }

/** IsMissing reports whether err is (or wraps) a missing-input error.
 *
 * Parameters:
 *   err (error) — any error
 *
 * Returns:
 *   bool — true for ErrMissing
 *
 * Example:
 *   if index.IsMissing(err) { os.Exit(66) }
 */
func IsMissing(err error) bool { return errors.Is(err, ErrMissing) }

/** IsAnalyzerError reports whether err is (or wraps) an unsupported-analyzer
 * error: the index was built with a word-splitting rule this reader lacks.
 *
 * Parameters:
 *   err (error) — any error
 *
 * Returns:
 *   bool — true for ErrAnalyzer
 *
 * Example:
 *   if index.IsAnalyzerError(err) { skipThisSet() }
 */
func IsAnalyzerError(err error) bool { return errors.Is(err, ErrAnalyzer) }

// errAnalyzer is the error for an index built with analyzer name: a data error,
// and an analyzer error.
func errAnalyzer(name string) error {
	return fmt.Errorf("%w: analyzer %q is not supported (this reader knows %s): %w", ErrData, name, Analyzer, ErrAnalyzer)
}

func dataf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrData, fmt.Sprintf(format, args...))
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// fileRef is [digest, size] in the manifest.
type fileRef struct {
	Digest string
	Size   int64
}

func (f fileRef) MarshalJSON() ([]byte, error) { return json.Marshal([]any{f.Digest, f.Size}) }

func (f *fileRef) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) < 2 {
		return dataf("a file entry is not [digest, size]")
	}
	if err := json.Unmarshal(raw[0], &f.Digest); err != nil || !digestRE.MatchString(f.Digest) {
		return dataf("bad digest in a file entry")
	}
	if err := json.Unmarshal(raw[1], &f.Size); err != nil || f.Size < 0 || f.Size > maxFileSize {
		return dataf("bad size in a file entry")
	}
	return nil
}

// chunkRef is [from, to, file].
type chunkRef struct {
	From, To string
	File     fileRef
}

func (c chunkRef) MarshalJSON() ([]byte, error) { return json.Marshal([]any{c.From, c.To, c.File}) }

func (c *chunkRef) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) < 3 {
		return dataf("a chunk entry is not [from, to, file]")
	}
	if json.Unmarshal(raw[0], &c.From) != nil || json.Unmarshal(raw[1], &c.To) != nil {
		return dataf("bad word bounds in a chunk entry")
	}
	return c.File.UnmarshalJSON(raw[2])
}

// rangeRef is [first, last, file].
type rangeRef struct {
	First, Last uint64
	File        fileRef
}

func (r rangeRef) MarshalJSON() ([]byte, error) { return json.Marshal([]any{r.First, r.Last, r.File}) }

func (r *rangeRef) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || len(raw) < 3 {
		return dataf("a range entry is not [first, last, file]")
	}
	if json.Unmarshal(raw[0], &r.First) != nil || json.Unmarshal(raw[1], &r.Last) != nil {
		return dataf("bad bounds in a range entry")
	}
	return r.File.UnmarshalJSON(raw[2])
}

type documentsRec struct {
	Count  uint64     `json:"count"`
	Words  uint64     `json:"words"`
	Ranges []rangeRef `json:"ranges"`
}

type manifest struct {
	Format       string       `json:"format"`
	Analyzer     string       `json:"analyzer"`
	Generation   uint64       `json:"generation"`
	Built        string       `json:"built"`
	Required     []string     `json:"required,omitempty"`
	DocumentOnly []string     `json:"documentOnly,omitempty"`
	Compression  string       `json:"compression"`
	Documents    documentsRec `json:"documents"`
	Chunks       []chunkRef   `json:"chunks"`
}

// posting is [delta, locations]; a reader ignores trailing elements (the
// reserved meta form), so decoding goes through raw elements.
type posting struct {
	Delta     uint64
	Locations []int64
}

func (p posting) MarshalCBOR() ([]byte, error) {
	return encMode.Marshal([]any{p.Delta, p.Locations})
}

func (p *posting) UnmarshalCBOR(b []byte) error {
	var raw []cbor.RawMessage
	if err := decMode.Unmarshal(b, &raw); err != nil || len(raw) < 2 {
		return dataf("a posting is not [delta, locations]")
	}
	if err := decMode.Unmarshal(raw[0], &p.Delta); err != nil {
		return dataf("bad posting delta")
	}
	if err := decMode.Unmarshal(raw[1], &p.Locations); err != nil {
		return dataf("bad posting locations")
	}
	return nil
}

type entry struct {
	_        struct{} `cbor:",toarray"`
	Word     string
	Postings []posting
}

type wordChunk struct {
	_          struct{} `cbor:",toarray"`
	Version    int
	Analyzer   string
	Generation uint64
	Entries    []entry
}

// documentRow is one page of a document table: its word count, the digest of
// its fragment file and, since the address was added, the page's address, so
// that a search learns the address of every hit without reading its text. An
// index written before has rows of two elements and an empty Address.
type documentRow struct {
	_         struct{} `cbor:",toarray"`
	WordCount uint64
	Fragment  []byte
	Address   string
}

type documentTable struct {
	_          struct{} `cbor:",toarray"`
	Version    int
	Analyzer   string
	Generation uint64
	Rows       []documentRow
}

type fragmentRec struct {
	Address   string            `cbor:"0,keyasint"`
	Text      string            `cbor:"1,keyasint"`
	WordCount uint64            `cbor:"2,keyasint"`
	Fields    map[string]any    `cbor:"3,keyasint,omitempty"`
	Anchors   []anchorRec       `cbor:"4,keyasint,omitempty"`
	Segments  [][2]uint64       `cbor:"5,keyasint,omitempty"`
	_         map[string]string `cbor:"-"`
}

var encMode, decMode = func() (cbor.EncMode, cbor.DecMode) {
	em, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	dm, err := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		MaxNestedLevels:  8,
		MaxArrayElements: maxFiles,
		MaxMapPairs:      100000,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return em, dm
}()

func compressedName(kind string, digest string, compression string) string {
	ext := ".cbor"
	if compression == "gzip" {
		ext = ".cbor.gz"
	}
	return path.Join(kind, digest+ext) // slash separated, as in a Source
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// encodeFile writes one CBOR value to dir/kind/ named by the digest of its
// uncompressed bytes and returns the file reference.
func encodeFile(dir, kind, compression string, v any) (fileRef, error) {
	return encodeFileLimit(dir, kind, compression, v, maxFileSize)
}

// encodeFileLimit is encodeFile with a smaller file limit (never a larger).
func encodeFileLimit(dir, kind, compression string, v any, limit int) (fileRef, error) {
	raw, err := encMode.Marshal(v)
	if err != nil {
		return fileRef{}, err
	}
	return writeFileLimit(dir, kind, compression, raw, limit)
}

func writeFile(dir, kind, compression string, raw []byte) (fileRef, error) {
	return writeFileLimit(dir, kind, compression, raw, maxFileSize)
}

func writeFileLimit(dir, kind, compression string, raw []byte, limit int) (fileRef, error) {
	if limit <= 0 || limit > maxFileSize {
		limit = maxFileSize
	}
	ref := fileRef{Digest: sum(raw), Size: int64(len(raw))}
	if ref.Size > int64(limit) {
		return ref, fmt.Errorf("%w: a file of %d bytes is over the %d byte limit", ErrTooLarge, ref.Size, limit)
	}
	data := raw
	if compression == "gzip" {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(raw)
		zw.Close()
		data = buf.Bytes()
	}
	file := filepath.Join(dir, filepath.FromSlash(compressedName(kind, ref.Digest, compression)))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return ref, err
	}
	return ref, replaceFile(file, data)
}

// replaceFile writes data to a temporary file beside name and renames it into
// place, so a reader that has the old file open or opens it now sees all of it:
// a rebuild rewrites a file whose content did not change under the same name.
func replaceFile(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), tempPrefix+"*")
	if err != nil {
		return err
	}
	tn := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tn, 0o644)
	}
	if err == nil {
		err = os.Rename(tn, name)
	}
	if err != nil {
		os.Remove(tn)
	}
	return err
}

// readFile reads the file kind/digest, decompresses with a limit, and checks
// size and digest.
func readFile(src Source, kind, compression string, ref fileRef) ([]byte, error) {
	name := compressedName(kind, ref.Digest, compression)
	f, err := src.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, dataf("the manifest names %s, which is missing", name)
		}
		return nil, err
	}
	defer f.Close()
	var r io.Reader = io.LimitReader(f, maxFileSize+1)
	if compression == "gzip" {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, dataf("%s is not gzip: %v", ref.Digest, err)
		}
		r = zr
	}
	limit := ref.Size
	if limit <= 0 || limit > maxFileSize {
		limit = maxFileSize
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, dataf("%s cannot be read: %v", ref.Digest, err)
	}
	if ref.Size > 0 && int64(len(raw)) != ref.Size {
		return nil, dataf("%s has %d bytes, the manifest says %d", ref.Digest, len(raw), ref.Size)
	}
	if sum(raw) != ref.Digest {
		return nil, dataf("%s does not match its digest", ref.Digest)
	}
	return raw, nil
}

func missingf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMissing, fmt.Sprintf(format, args...))
}

// anchorRec is a heading: [slug, heading, position, level].
type anchorRec struct {
	_        struct{} `cbor:",toarray"`
	Slug     string
	Heading  string
	Position uint64
	Level    uint64
}

// decodeRecord reads a CBOR array into its fields in order and ignores unknown
// trailing elements, as the version rule in format.cddl says a version-1
// reader does. A record with fewer elements than fields is wrong. what names
// the record in the error.
func decodeRecord(b []byte, what string, fields ...any) error {
	var raw []cbor.RawMessage
	if err := decMode.Unmarshal(b, &raw); err != nil || len(raw) < len(fields) {
		return dataf("%s is not an array of at least %d elements", what, len(fields))
	}
	for i, f := range fields {
		if err := decMode.Unmarshal(raw[i], f); err != nil {
			return dataf("%s element %d: %v", what, i, err)
		}
	}
	return nil
}

func (e *entry) UnmarshalCBOR(b []byte) error {
	return decodeRecord(b, "a word entry", &e.Word, &e.Postings)
}

func (c *wordChunk) UnmarshalCBOR(b []byte) error {
	return decodeRecord(b, "a word chunk", &c.Version, &c.Analyzer, &c.Generation, &c.Entries)
}

// UnmarshalCBOR reads a row of two or more elements: the word count and the
// fragment digest, then the address when there is one (a row of an older index
// has none), and ignores what follows, as the version rule says.
func (r *documentRow) UnmarshalCBOR(b []byte) error {
	if err := decodeRecord(b, "a document row", &r.WordCount, &r.Fragment); err != nil {
		return err
	}
	var raw []cbor.RawMessage
	if err := decMode.Unmarshal(b, &raw); err != nil {
		return dataf("a document row is not an array")
	}
	r.Address = ""
	if len(raw) >= 3 {
		if err := decMode.Unmarshal(raw[2], &r.Address); err != nil {
			return dataf("a document row's address is not text: %v", err)
		}
	}
	return nil
}

func (d *documentTable) UnmarshalCBOR(b []byte) error {
	return decodeRecord(b, "a document table", &d.Version, &d.Analyzer, &d.Generation, &d.Rows)
}

func (a *anchorRec) UnmarshalCBOR(b []byte) error {
	return decodeRecord(b, "an anchor", &a.Slug, &a.Heading, &a.Position, &a.Level)
}
