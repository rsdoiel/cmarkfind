package index

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
 *
 * Example:
 *   opt := index.BuildOptions{Generation: 3}
 */
type BuildOptions struct {
	ChunkEntries int
	Generation   uint64
	Built        time.Time
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
	if opt.ChunkEntries <= 0 {
		opt.ChunkEntries = DefaultChunkEntries
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
	for n, d := range docs {
		parts := []struct {
			text   string
			weight int
		}{{d.Title, weightTitle}, {d.Description, weightDescription}, {d.Body, weightBody}}
		var surfaces []string
		var segments [][2]uint64
		var occ = map[string][]occurrence{}
		var anchors []anchorRec
		seen := map[string]int{}
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
				slug := Slug(h.text)
				if slug == "" || len(Words(h.text)) == 0 {
					continue
				}
				if k := seen[slug]; k > 0 {
					seen[slug]++
					slug = fmt.Sprintf("%s-%d", slug, k)
				} else {
					seen[slug] = 1
				}
				anchors = append(anchors, anchorRec{Slug: slug, Heading: h.text, Position: uint64(base + before), Level: uint64(h.level)})
			}
			segments = append(segments, [2]uint64{uint64(p.weight), uint64(len(ws))})
			for _, w := range ws {
				occ[w.Term] = append(occ[w.Term], occurrence{pos: len(surfaces), weight: p.weight})
				surfaces = append(surfaces, w.Surface)
			}
		}
		for term, os := range occ {
			postings[term] = append(postings[term], rawPosting{doc: n, locs: encodeLocations(os)})
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
		ref, err := encodeFile(dir, "f", compression, frag)
		if err != nil {
			return err
		}
		digest, _ := hexDecode(ref.Digest)
		rows = append(rows, documentRow{WordCount: uint64(len(surfaces)), Fragment: digest})
		totalWords += uint64(len(surfaces))
	}

	m := manifest{Format: Format, Analyzer: Analyzer, Generation: opt.Generation,
		Built: opt.Built.UTC().Format(time.RFC3339), Compression: compression,
		Documents: documentsRec{Count: uint64(len(docs)), Words: totalWords}, Chunks: []chunkRef{}}

	for first := 0; first < len(rows); first += docsPerTable {
		last := min(first+docsPerTable, len(rows))
		ref, err := encodeFile(dir, "d", compression, documentTable{Version: 1, Analyzer: Analyzer, Generation: opt.Generation, Rows: rows[first:last]})
		if err != nil {
			return err
		}
		m.Documents.Ranges = append(m.Documents.Ranges, rangeRef{First: uint64(first), Last: uint64(last - 1), File: ref})
	}

	words := make([]string, 0, len(postings))
	for w := range postings {
		words = append(words, w)
	}
	sort.Strings(words)
	var cur []entry
	count := 0
	flush := func() error {
		if len(cur) == 0 {
			return nil
		}
		ref, err := encodeFile(dir, "w", compression, wordChunk{Version: 1, Analyzer: Analyzer, Generation: opt.Generation, Entries: cur})
		if err != nil {
			return err
		}
		m.Chunks = append(m.Chunks, chunkRef{From: cur[0].Word, To: cur[len(cur)-1].Word, File: ref})
		cur, count = nil, 0
		return nil
	}
	for _, w := range words {
		var ps []posting
		prev := 0
		for _, rp := range postings[w] {
			ps = append(ps, posting{Delta: uint64(rp.doc - prev), Locations: rp.locs})
			prev = rp.doc
			for _, l := range rp.locs {
				if l >= 0 {
					count++
				}
			}
		}
		cur = append(cur, entry{Word: w, Postings: ps})
		if count >= opt.ChunkEntries {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(out, '\n'), 0o644)
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
