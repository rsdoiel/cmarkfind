package index

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// BM25 constants (the Pagefind defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

/** Term is one word of a query.
 *
 * Fields:
 *   Text (string)  — the word, lower case
 *   Prefix (bool)  — true matches every word that starts with Text
 *
 * Example:
 *   index.Term{Text: "tomato", Prefix: true}
 */
type Term struct {
	Text   string
	Prefix bool
}

/** Query is a version-1 query: words that must all match (implicit AND) and
 * phrases that must all occur. Words are lower-cased by the caller (use Words).
 *
 * Fields:
 *   Terms ([]Term)       — each must match
 *   Phrases ([][]string) — each must occur as consecutive words of one field
 *
 * Example:
 *   q := index.Query{Terms: []index.Term{{Text: "soil"}}}
 */
type Query struct {
	Terms   []Term
	Phrases [][]string
}

/** Hit is one result.
 *
 * Fields:
 *   Doc (int)        — document number in this index
 *   Address (string) — the page's address
 *   Score (float64)  — BM25 sum; higher is better
 *
 * Example:
 *   for _, h := range hits { fmt.Println(h.Address, h.Score) }
 */
type Hit struct {
	Doc     int
	Address string
	Score   float64
}

/** Fragment is a stored page: what a result and its excerpt are made of.
 *
 * Fields:
 *   Address, Title, Description (string) — as given to the writer
 *   Text (string)       — one surface word per word of the page
 *   WordCount (int)     — number of words of Text
 *   Segments ([][2]int) — [weight, words] pieces of Text (title, description, body)
 *
 * Example:
 *   f, _ := ix.Fragment(0)
 */
type Fragment struct {
	Address, Title, Description, Text string
	WordCount                         int
	Segments                          [][2]int
}

/** Index is an opened index directory. Files are read when a query needs them,
 * each checked against its digest. It is safe for concurrent use.
 */
type Index struct {
	dir string
	m   manifest

	mu     sync.Mutex
	chunks map[int][]entry
	rows   []documentRow // all document rows, loaded on first use
	frags  map[int]*fragmentRec
}

/** Open reads and checks the manifest in dir. It refuses an unknown format,
 * analyzer, compression or required feature, and bad chunk bounds.
 *
 * Parameters:
 *   dir (string) — the index directory
 *
 * Returns:
 *   *Index — the opened index
 *   error  — ErrMissing when there is no manifest; ErrData when it is wrong
 *
 * Example:
 *   ix, err := index.Open("idx")
 */
func Open(dir string) (*Index, error) {
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, wrapMissing(dir)
		}
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxManifestSize {
		return nil, dataf("manifest is over %d bytes", maxManifestSize)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		if IsDataError(err) {
			return nil, err
		}
		return nil, dataf("manifest.json: %v", err)
	}
	if m.Format != Format {
		return nil, dataf("format %q is not supported (this reader knows %s)", m.Format, Format)
	}
	if m.Analyzer != Analyzer {
		return nil, dataf("analyzer %q is not supported (this reader knows %s)", m.Analyzer, Analyzer)
	}
	if len(m.Required) > 0 {
		return nil, dataf("the index requires features this reader lacks: %s", strings.Join(m.Required, ", "))
	}
	if m.Compression != "gzip" && m.Compression != "none" {
		return nil, dataf("compression %q is not supported", m.Compression)
	}
	if len(m.Chunks)+len(m.Documents.Ranges) > maxFiles {
		return nil, dataf("manifest names too many files")
	}
	for i, c := range m.Chunks {
		if c.From > c.To || (i > 0 && c.From <= m.Chunks[i-1].To) {
			return nil, dataf("chunk %d is out of order", i)
		}
	}
	var next uint64
	for _, r := range m.Documents.Ranges {
		if r.First != next || r.Last < r.First {
			return nil, dataf("document ranges are not contiguous")
		}
		next = r.Last + 1
	}
	if next != m.Documents.Count {
		return nil, dataf("document ranges cover %d documents, the manifest says %d", next, m.Documents.Count)
	}
	return &Index{dir: dir, m: m, chunks: map[int][]entry{}, frags: map[int]*fragmentRec{}}, nil
}

func wrapMissing(dir string) error {
	return missingf("no index at %s (manifest.json not found)", dir)
}

/** Count returns the number of documents.
 *
 * Returns:
 *   int — documents in the index
 *
 * Example:
 *   n := ix.Count()
 */
func (ix *Index) Count() int { return int(ix.m.Documents.Count) }

/** Chunks returns the number of word chunks.
 *
 * Returns:
 *   int — chunk files the manifest names
 *
 * Example:
 *   n := ix.Chunks()
 */
func (ix *Index) Chunks() int { return len(ix.m.Chunks) }

/** Generation returns the build generation recorded in the manifest.
 *
 * Returns:
 *   uint64 — the generation
 *
 * Example:
 *   g := ix.Generation()
 */
func (ix *Index) Generation() uint64 { return ix.m.Generation }

func (ix *Index) chunk(i int) ([]entry, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if e, ok := ix.chunks[i]; ok {
		return e, nil
	}
	raw, err := readFile(ix.dir, "w", ix.m.Compression, ix.m.Chunks[i].File)
	if err != nil {
		return nil, err
	}
	var wc wordChunk
	if err := decMode.Unmarshal(raw, &wc); err != nil {
		return nil, dataf("word chunk %s: %v", ix.m.Chunks[i].File.Digest, err)
	}
	if wc.Version != 1 || wc.Analyzer != Analyzer || wc.Generation != ix.m.Generation {
		return nil, dataf("word chunk %s: header differs from the manifest", ix.m.Chunks[i].File.Digest)
	}
	ix.chunks[i] = wc.Entries
	return wc.Entries, nil
}

func (ix *Index) docRows() ([]documentRow, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.rows != nil {
		return ix.rows, nil
	}
	rows := make([]documentRow, 0, ix.m.Documents.Count)
	for _, r := range ix.m.Documents.Ranges {
		raw, err := readFile(ix.dir, "d", ix.m.Compression, r.File)
		if err != nil {
			return nil, err
		}
		var dt documentTable
		if err := decMode.Unmarshal(raw, &dt); err != nil {
			return nil, dataf("document table %s: %v", r.File.Digest, err)
		}
		if dt.Version != 1 || dt.Analyzer != Analyzer || dt.Generation != ix.m.Generation {
			return nil, dataf("document table %s: header differs from the manifest", r.File.Digest)
		}
		if uint64(len(dt.Rows)) != r.Last-r.First+1 {
			return nil, dataf("document table %s has %d rows, the manifest says %d", r.File.Digest, len(dt.Rows), r.Last-r.First+1)
		}
		rows = append(rows, dt.Rows...)
	}
	ix.rows = rows
	return rows, nil
}

func (ix *Index) fragment(doc int) (*fragmentRec, error) {
	rows, err := ix.docRows()
	if err != nil {
		return nil, err
	}
	if doc < 0 || doc >= len(rows) {
		return nil, dataf("no document %d", doc)
	}
	ix.mu.Lock()
	if f, ok := ix.frags[doc]; ok {
		ix.mu.Unlock()
		return f, nil
	}
	ix.mu.Unlock()
	if len(rows[doc].Fragment) != 32 {
		return nil, dataf("document %d has a bad fragment digest", doc)
	}
	raw, err := readFile(ix.dir, "f", ix.m.Compression, fileRef{Digest: hex.EncodeToString(rows[doc].Fragment)})
	if err != nil {
		return nil, err
	}
	var fr fragmentRec
	if err := decMode.Unmarshal(raw, &fr); err != nil {
		return nil, dataf("fragment of document %d: %v", doc, err)
	}
	ix.mu.Lock()
	ix.frags[doc] = &fr
	ix.mu.Unlock()
	return &fr, nil
}

/** Fragment returns the stored page of document number doc.
 *
 * Parameters:
 *   doc (int) — a document number from a Hit
 *
 * Returns:
 *   Fragment — the page's stored text and fields
 *   error    — ErrData when the fragment is missing or does not match its digest
 *
 * Example:
 *   f, err := ix.Fragment(hit.Doc)
 */
func (ix *Index) Fragment(doc int) (Fragment, error) {
	fr, err := ix.fragment(doc)
	if err != nil {
		return Fragment{}, err
	}
	out := Fragment{Address: fr.Address, Text: fr.Text, WordCount: int(fr.WordCount)}
	out.Title, _ = fr.Fields["title"].(string)
	out.Description, _ = fr.Fields["description"].(string)
	for _, s := range fr.Segments {
		out.Segments = append(out.Segments, [2]int{int(s[0]), int(s[1])})
	}
	return out, nil
}

// termPostings is a document's weighted use of one word: positions with weights.
type use struct {
	pos    int
	weight int
}

// lookup returns, for each document, the uses of every word matched by the
// term (an exact word or, with Prefix, every word with that prefix).
func (ix *Index) lookup(t Term) (map[int][]use, error) {
	from := sort.Search(len(ix.m.Chunks), func(i int) bool { return ix.m.Chunks[i].To >= t.Text })
	out := map[int][]use{}
	for i := from; i < len(ix.m.Chunks); i++ {
		c := ix.m.Chunks[i]
		if !t.Prefix && c.From > t.Text {
			break
		}
		if t.Prefix && c.From > t.Text && !strings.HasPrefix(c.From, t.Text) {
			break
		}
		entries, err := ix.chunk(i)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if t.Prefix && !strings.HasPrefix(e.Word, t.Text) || !t.Prefix && e.Word != t.Text {
				continue
			}
			doc := 0
			for _, p := range e.Postings {
				doc += int(p.Delta)
				out[doc] = append(out[doc], decodeLocations(p.Locations)...)
			}
		}
	}
	return out, nil
}

func decodeLocations(locs []int64) []use {
	var out []use
	weight, pos := 1, 0
	for _, l := range locs {
		if l < 0 {
			weight = int(-l)
			continue
		}
		pos += int(l)
		out = append(out, use{pos: pos, weight: weight})
	}
	return out
}

/** Search runs q and returns up to limit hits, best first (ties by document
 * number). Every term and phrase must match; the score is the BM25 sum over
 * the matched words with field weights 4, 2 and 1.
 *
 * Parameters:
 *   q (Query)    — words and phrases
 *   limit (int)  — most hits to return; 0 or less means no limit
 *
 * Returns:
 *   []Hit — matches, best first; empty (not an error) when nothing matches
 *   error — ErrData when a file does not match its digest or is malformed
 *
 * Example:
 *   hits, err := ix.Search(index.Query{Terms: []index.Term{{Text: "soil"}}}, 10)
 */
func (ix *Index) Search(q Query, limit int) ([]Hit, error) {
	if len(q.Terms) == 0 && len(q.Phrases) == 0 {
		return nil, nil
	}
	rows, err := ix.docRows()
	if err != nil {
		return nil, err
	}
	n := float64(len(rows))
	avg := float64(ix.m.Documents.Words) / math.Max(n, 1)
	if avg == 0 {
		avg = 1
	}

	var sets []map[int][]use
	for _, t := range q.Terms {
		u, err := ix.lookup(t)
		if err != nil {
			return nil, err
		}
		sets = append(sets, u)
	}
	phraseWords := make([][]map[int][]use, len(q.Phrases))
	for i, ph := range q.Phrases {
		if len(ph) == 0 {
			continue
		}
		for _, w := range ph {
			u, err := ix.lookup(Term{Text: w})
			if err != nil {
				return nil, err
			}
			phraseWords[i] = append(phraseWords[i], u)
		}
	}

	// candidates: documents present in every set
	cands := map[int]bool{}
	first := true
	narrow := func(has func(int) bool, universe map[int][]use) {
		if first {
			for d := range universe {
				cands[d] = true
			}
			first = false
			return
		}
		for d := range cands {
			if !has(d) {
				delete(cands, d)
			}
		}
	}
	for _, s := range sets {
		s := s
		narrow(func(d int) bool { _, ok := s[d]; return ok }, s)
	}
	for _, ws := range phraseWords {
		if len(ws) == 0 {
			continue
		}
		for _, w := range ws {
			w := w
			narrow(func(d int) bool { _, ok := w[d]; return ok }, w)
		}
	}

	scores := map[int]float64{}
	for d := range cands {
		var s float64
		for _, set := range sets {
			s += bm25(set[d], len(set), n, float64(rows[d].WordCount), avg)
		}
		for i, ws := range phraseWords {
			if len(ws) == 0 {
				continue
			}
			ok, err := ix.phraseAt(d, ws)
			if err != nil {
				return nil, err
			}
			if !ok {
				delete(cands, d)
				s = 0
				break
			}
			_ = i
			for _, w := range ws {
				s += bm25(w[d], len(w), n, float64(rows[d].WordCount), avg)
			}
		}
		if cands[d] {
			scores[d] = s
		}
	}
	hits := make([]Hit, 0, len(scores))
	for d, s := range scores {
		hits = append(hits, Hit{Doc: d, Score: s})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Doc < hits[j].Doc
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	for i := range hits {
		fr, err := ix.fragment(hits[i].Doc)
		if err != nil {
			return nil, err
		}
		hits[i].Address = fr.Address
	}
	return hits, nil
}

func bm25(uses []use, df int, n, length, avg float64) float64 {
	var tf float64
	for _, u := range uses {
		tf += float64(u.weight)
	}
	if tf == 0 {
		return 0
	}
	idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
	return idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*length/avg))
}

// phraseAt reports whether the words occur consecutively in document d, all
// inside one segment (title, description or body).
func (ix *Index) phraseAt(d int, ws []map[int][]use) (bool, error) {
	fr, err := ix.fragment(d)
	if err != nil {
		return false, err
	}
	segOf := func(pos int) int {
		start := 0
		for i, s := range fr.Segments {
			if pos < start+int(s[1]) {
				return i
			}
			start += int(s[1])
		}
		return -1
	}
	for _, u := range ws[0][d] {
		ok := true
		for k := 1; k < len(ws) && ok; k++ {
			found := false
			for _, v := range ws[k][d] {
				if v.pos == u.pos+k {
					found = true
					break
				}
			}
			ok = found
		}
		if ok && segOf(u.pos) == segOf(u.pos+len(ws)-1) {
			return true, nil
		}
	}
	return false, nil
}

/** Verify checks an index directory end to end: the manifest, then every file
 * it names against its size and digest, every fragment the document tables
 * name, and each fragment's word count against its table row.
 *
 * Parameters:
 *   dir (string) — the index directory
 *
 * Returns:
 *   error — nil when sound; ErrMissing without a manifest; ErrData otherwise
 *
 * Example:
 *   if err := index.Verify("idx"); err != nil { os.Exit(65) }
 */
func Verify(dir string) error {
	ix, err := Open(dir)
	if err != nil {
		return err
	}
	for i := range ix.m.Chunks {
		if _, err := ix.chunk(i); err != nil {
			return err
		}
	}
	rows, err := ix.docRows()
	if err != nil {
		return err
	}
	for d, r := range rows {
		fr, err := ix.fragment(d)
		if err != nil {
			return err
		}
		if fr.WordCount != r.WordCount {
			return dataf("document %d: fragment has %d words, the table says %d", d, fr.WordCount, r.WordCount)
		}
	}
	return nil
}
