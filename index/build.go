package index

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rsdoiel/markdownweb/slug"
)

// DefaultChunkEntries is the default number of entries (a posting plus one
// position each) at which a word chunk is cut.
const DefaultChunkEntries = 10000

const docsPerTable = 5000

/** Doc is one page given to the writer.
 *
 * Fields:
 *   Address (string)     — the page's published address; unique in an index
 *   Title (string)       — searched with weight 4
 *   Description (string) — searched with weight 2
 *   Body (string)        — the page's Markdown, front matter removed; weight 1
 *
 * Example:
 *   d := index.Doc{Address: "https://example.org/a.md", Title: "A", Body: "text"}
 */
type Doc struct {
	Address, Title, Description, Body string
}

/** BuildOptions are the build settings; none is part of the format.
 *
 * Fields:
 *   ChunkEntries (int) — entries per word chunk; 0 means DefaultChunkEntries
 *   Generation (uint64) — grows with every build of the same index; 0 means 1
 *   Built (time.Time)   — recorded in the manifest; zero means now
 *   EntryBytes (int)    — encoded size over which a word is written document-only;
 *                         0 means DefaultEntryBytes
 *   FileLimit (int)     — largest file the build writes; 0 or more than the format's
 *                         8 MiB means 8 MiB (smaller values are for tests)
 *
 * A word on more than a million pages fails the build (ErrTooLarge, naming the
 * word and its page count): a reader refuses a longer posting list.
 *
 * Example:
 *   opt := index.BuildOptions{Generation: 3}
 */
type BuildOptions struct {
	ChunkEntries int
	Generation   uint64
	Built        time.Time
	EntryBytes   int
	FileLimit    int
	postingCap   int // pages one word may be on; 0 means defaultPostingCap (tests lower it)
}

// defaultPostingCap is the most pages one word may be on: a word's posting list
// is one CBOR array and the reader refuses an array of more than maxFiles
// elements. The build refuses first, naming the word, so that it does not write
// an index every search would call damaged.
const defaultPostingCap = maxFiles

/** DocumentOnlyWord is a word written without positions.
 *
 * Fields:
 *   Word (string)    — the term
 *   Pages (int)      — pages that hold it
 *   Positions (int)  — occurrences it had, all pages together
 *
 * Example:
 *   w := index.DocumentOnlyWord{Word: "the", Pages: 328, Positions: 64647}
 */
type DocumentOnlyWord struct {
	Word             string
	Pages, Positions int
}

/** SkippedPage is a page the build left out of the index because its text
 * does not fit one fragment file.
 *
 * Fields:
 *   Address (string) — the page
 *   Bytes (int64)    — the size its fragment would have had, uncompressed
 *   Limit (int)      — the file limit it went over
 *
 * Example:
 *   s := index.SkippedPage{Address: "https://example.org/novel.md", Bytes: 8800115, Limit: 8388608}
 */
type SkippedPage struct {
	Address string
	Bytes   int64
	Limit   int
}

/** BuildReport says what a build did that the person should know about.
 *
 * Fields:
 *   DocumentOnly ([]DocumentOnlyWord) — words written without positions, by word
 *   LongTokens (int)                  — words over MaxTermBytes left out of the index
 *   LargestEntryBytes (int)           — encoded size of the biggest entry written
 *   LargestEntryWord (string)         — that entry's word
 *   Warnings ([]string)               — files over half the file limit and pages left
 *                                       out of the index, one line each
 *   Skipped ([]SkippedPage)           — pages whose text is over one fragment file's limit
 *
 * Example:
 *   rep, err := index.BuildWithReport(dir, docs, index.BuildOptions{})
 */
type BuildReport struct {
	DocumentOnly      []DocumentOnlyWord
	LongTokens        int
	LargestEntryBytes int
	LargestEntryWord  string
	Warnings          []string
	Skipped           []SkippedPage
}

type occurrence struct {
	pos    int
	weight int
}

type rawPosting struct {
	doc  int
	locs []int64
}

/** Build writes the index of docs into dir: word chunks, document tables,
 * fragments and manifest.json. The same docs (in any order) and options give
 * the same bytes. An existing index in dir is not removed; files not named by
 * the new manifest are left for the caller to prune.
 *
 * Parameters:
 *   dir (string)          — the directory to write; created when missing
 *   docs ([]Doc)          — the pages; addresses must be unique
 *   opt (BuildOptions)    — settings
 *
 * Returns:
 *   error — nil on success; ErrData for duplicate addresses
 *
 * Example:
 *   err := index.Build("idx", docs, index.BuildOptions{})
 */
func Build(dir string, docs []Doc, opt BuildOptions) error {
	_, err := BuildWithReport(dir, docs, opt)
	return err
}

/** BuildWithReport is Build that also says which words it wrote without
 * positions (a word whose entry is over opt.EntryBytes keeps the pages it is
 * on and the fields it is in, not where), how many words were too long to
 * index, and which files came near the file limit. A build that cannot fit
 * writes no manifest and returns an error wrapping ErrTooLarge that names the
 * word and its page count; an index already in dir is left as it was.
 *
 * Parameters:
 *   dir (string)          — the directory to write; created when missing
 *   docs ([]Doc)          — the pages; addresses must be unique
 *   opt (BuildOptions)    — settings
 *
 * Returns:
 *   *BuildReport — what the build did (also when it failed part way)
 *   error        — nil on success; ErrData for duplicate addresses; ErrTooLarge
 *
 * Example:
 *   rep, err := index.BuildWithReport("idx", docs, index.BuildOptions{})
 */
func BuildWithReport(dir string, docs []Doc, opt BuildOptions) (*BuildReport, error) {
	rep := &BuildReport{}
	return rep, buildInto(dir, docs, opt, rep)
}

func buildInto(dir string, docs []Doc, opt BuildOptions, rep *BuildReport) error {
	if opt.EntryBytes <= 0 {
		opt.EntryBytes = DefaultEntryBytes
	}
	if opt.FileLimit <= 0 || opt.FileLimit > maxFileSize {
		opt.FileLimit = maxFileSize
	}
	// over half the limit is worth a warning; only the biggest file of a kind is named
	type big struct {
		size int64
		what string
	}
	biggest := map[string]big{}
	note := func(kind string, size int64, what string) {
		if size > biggest[kind].size {
			biggest[kind] = big{size, what}
		}
	}
	if opt.ChunkEntries <= 0 {
		opt.ChunkEntries = DefaultChunkEntries
	}
	if opt.postingCap <= 0 {
		opt.postingCap = defaultPostingCap
	}
	if opt.Generation == 0 {
		opt.Generation = 1
	}
	if opt.Built.IsZero() {
		opt.Built = time.Now()
	}
	const compression = "gzip"
	docs = append([]Doc(nil), docs...)
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Address < docs[j].Address })
	for i := 1; i < len(docs); i++ {
		if docs[i].Address == docs[i-1].Address {
			return dataf("two pages have the address %s", docs[i].Address)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	postings := map[string][]rawPosting{}
	var rows []documentRow
	var totalWords uint64
	for _, d := range docs {
		longBefore := rep.LongTokens
		parts := []struct {
			text   string
			weight int
		}{{d.Title, weightTitle}, {d.Description, weightDescription}, {d.Body, weightBody}}
		var surfaces []string
		var segments [][2]uint64
		var occ = map[string][]occurrence{}
		var anchors []anchorRec
		slugs := slug.New()
		for _, p := range parts {
			plain, heads := markdownText(p.text)
			ws := Words(plain)
			if len(ws) == 0 {
				continue
			}
			base, prev, before := len(surfaces), 0, 0
			for _, h := range heads {
				before += len(Words(plain[prev:h.offset]))
				prev = h.offset
				if len(Words(h.text)) == 0 {
					continue
				}
				sl := slugs.Next(h.text)
				if sl == "" {
					continue
				}
				anchors = append(anchors, anchorRec{Slug: sl, Heading: h.text, Position: uint64(base + before), Level: uint64(h.level)})
			}
			segments = append(segments, [2]uint64{uint64(p.weight), uint64(len(ws))})
			for _, w := range ws {
				if len(w.Term) > MaxTermBytes {
					rep.LongTokens++ // not indexed; still a word of the page, so positions hold
				} else {
					occ[w.Term] = append(occ[w.Term], occurrence{pos: len(surfaces), weight: p.weight})
				}
				surfaces = append(surfaces, w.Surface)
			}
		}
		fields := map[string]any{}
		if d.Title != "" {
			fields["title"] = d.Title
		}
		if d.Description != "" {
			fields["description"] = d.Description
		}
		frag := fragmentRec{Address: d.Address, Text: strings.Join(surfaces, " "), WordCount: uint64(len(surfaces)), Segments: segments}
		if len(fields) > 0 {
			frag.Fields = fields
		}
		frag.Anchors = anchors
		ref, err := encodeFileLimit(dir, "f", compression, frag, opt.FileLimit)
		if errors.Is(err, ErrTooLarge) {
			// One page too big for a fragment file is left out, not fatal: it
			// would otherwise stop every search of the corpus. A word chunk or
			// a document table over the limit still fails the build below.
			rep.LongTokens = longBefore
			rep.Skipped = append(rep.Skipped, SkippedPage{Address: d.Address, Bytes: ref.Size, Limit: opt.FileLimit})
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("the page %s is not in the index: its text is %d bytes, over the %d byte limit of one index file", d.Address, ref.Size, opt.FileLimit))
			continue
		}
		if err != nil {
			return fmt.Errorf("the page %s: %w", d.Address, err)
		}
		doc := len(rows) // pages left out do not take a number
		for term, os := range occ {
			postings[term] = append(postings[term], rawPosting{doc: doc, locs: encodeLocations(os)})
		}
		note("fragment", ref.Size, "the text of "+d.Address)
		digest, _ := hexDecode(ref.Digest)
		rows = append(rows, documentRow{WordCount: uint64(len(surfaces)), Fragment: digest})
		totalWords += uint64(len(surfaces))
	}

	m := manifest{Format: Format, Analyzer: Analyzer, Generation: opt.Generation,
		Built: opt.Built.UTC().Format(time.RFC3339), Compression: compression,
		Documents: documentsRec{Count: uint64(len(rows)), Words: totalWords}, Chunks: []chunkRef{}}

	for first := 0; first < len(rows); first += docsPerTable {
		last := min(first+docsPerTable, len(rows))
		ref, err := encodeFileLimit(dir, "d", compression, documentTable{Version: 1, Analyzer: Analyzer, Generation: opt.Generation, Rows: rows[first:last]}, opt.FileLimit)
		if err != nil {
			return fmt.Errorf("a document table of %d pages: %w", last-first, err)
		}
		note("document table", ref.Size, fmt.Sprintf("a table of %d pages", last-first))
		m.Documents.Ranges = append(m.Documents.Ranges, rangeRef{First: uint64(first), Last: uint64(last - 1), File: ref})
	}

	words := make([]string, 0, len(postings))
	for w := range postings {
		words = append(words, w)
	}
	sort.Strings(words)
	var cur []entry
	var curPages []int // pages and positions of each entry of cur, for the error text
	count := 0
	flush := func() error {
		if len(cur) == 0 {
			return nil
		}
		ref, err := encodeFileLimit(dir, "w", compression, wordChunk{Version: 1, Analyzer: Analyzer, Generation: opt.Generation, Entries: cur}, opt.FileLimit)
		if err != nil {
			if len(cur) == 1 {
				return fmt.Errorf("the word %q in %d pages: %w", cur[0].Word, curPages[0], err)
			}
			return fmt.Errorf("the words %q to %q: %w", cur[0].Word, cur[len(cur)-1].Word, err)
		}
		what := fmt.Sprintf("words %q to %q", cur[0].Word, cur[len(cur)-1].Word)
		if len(cur) == 1 {
			what = fmt.Sprintf("the word %q in %d pages", cur[0].Word, curPages[0])
		}
		note("word chunk", ref.Size, what)
		m.Chunks = append(m.Chunks, chunkRef{From: cur[0].Word, To: cur[len(cur)-1].Word, File: ref})
		cur, curPages, count = nil, nil, 0
		return nil
	}
	for _, w := range words {
		if n := len(postings[w]); n > opt.postingCap {
			return fmt.Errorf("the word %q is on %d pages, over the %d pages one word may be on: %w", w, n, opt.postingCap, ErrTooLarge)
		}
		var ps []posting
		prev, entryCount, positions := 0, 0, 0
		for _, rp := range postings[w] {
			ps = append(ps, posting{Delta: uint64(rp.doc - prev), Locations: rp.locs})
			prev = rp.doc
			entryCount++ // the posting itself, then one for each position
			for _, l := range rp.locs {
				if l >= 0 {
					entryCount++
					positions++
				}
			}
		}
		e := entry{Word: w, Postings: ps}
		// An entry costs at least a byte a position and at most nine: only a
		// big one is encoded to find its size.
		size := 0
		if entryCount*9 > opt.EntryBytes {
			raw, err := encMode.Marshal(e)
			if err != nil {
				return err
			}
			size = len(raw)
		}
		if size > opt.EntryBytes {
			e = documentOnlyEntry(e)
			entryCount = len(e.Postings)
			for _, p := range e.Postings {
				entryCount += len(p.Locations)
			}
			m.DocumentOnly = append(m.DocumentOnly, w)
			rep.DocumentOnly = append(rep.DocumentOnly, DocumentOnlyWord{Word: w, Pages: len(ps), Positions: positions})
			if raw, err := encMode.Marshal(e); err == nil {
				size = len(raw)
			}
		}
		if size > rep.LargestEntryBytes {
			rep.LargestEntryBytes, rep.LargestEntryWord = size, w
		}
		cur = append(cur, e)
		curPages = append(curPages, len(ps))
		count += entryCount
		if count >= opt.ChunkEntries {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if len(m.DocumentOnly) > 0 {
		m.Required = append(m.Required, FeatureDocumentOnly)
	}
	for kind, b := range biggest {
		if b.size > int64(opt.FileLimit)/2 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("the largest %s file (%s) is %d bytes, over half of the %d byte limit", kind, b.what, b.size, opt.FileLimit))
		}
	}
	sort.Strings(rep.Warnings)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return replaceManifest(dir, append(out, '\n'))
}

// documentOnlyEntry rewrites an entry without positions: each posting keeps
// the fields the word occurs in as a strictly decreasing list of their weights
// (4 title, 2 description, 1 body), in place of its locations.
func documentOnlyEntry(e entry) entry {
	out := entry{Word: e.Word, Postings: make([]posting, len(e.Postings))}
	for i, p := range e.Postings {
		seen := map[int]bool{}
		for _, u := range decodeLocations(p.Locations) {
			seen[u.weight] = true
		}
		ws := make([]int64, 0, len(seen))
		for w := range seen {
			ws = append(ws, int64(w))
		}
		sort.Slice(ws, func(a, b int) bool { return ws[a] > ws[b] })
		out.Postings[i] = posting{Delta: p.Delta, Locations: ws}
	}
	return out
}

// replaceManifest replaces manifest.json whole: the bytes go to a temporary file
// in the same directory, then it is renamed over the manifest, so a reader
// sees the old manifest or the new one and never half of either.
func replaceManifest(dir string, b []byte) error {
	return replaceFile(filepath.Join(dir, "manifest.json"), b)
}

// encodeLocations delta-codes positions; a negative integer -w switches the
// weight of the positions after it (the default is 1).
func encodeLocations(os []occurrence) []int64 {
	var out []int64
	weight, prev := 1, 0
	for _, o := range os {
		if o.weight != weight {
			out = append(out, -int64(o.weight))
			weight = o.weight
		}
		out = append(out, int64(o.pos-prev))
		prev = o.pos
	}
	return out
}

func hexDecode(s string) ([]byte, error) {
	if len(s) != 2*sha256.Size {
		return nil, fmt.Errorf("bad digest")
	}
	out := make([]byte, sha256.Size)
	for i := range out {
		var b byte
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &b); err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}
