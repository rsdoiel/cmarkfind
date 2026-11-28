package index

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

// BM25 constants (the Pagefind defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

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
 *   Anchors ([]Anchor)  — the page's headings, in order
 *
 * Example:
 *   f, _ := ix.Fragment(0)
 */
type Fragment struct {
	Address, Title, Description, Text string
	WordCount                         int
	Segments                          [][2]int
	Anchors                           []Anchor
}

/** Anchor is a heading of a page.
 *
 * Fields:
 *   Slug (string)    — fragment identifier, unique on the page
 *   Heading (string) — the heading text
 *   Position (int)   — index of its first word in the fragment text
 *   Level (int)      — 1 to 6
 *
 * Example:
 *   link := f.Address + "#" + f.Anchors[0].Slug
 */
type Anchor struct {
	Slug, Heading   string
	Position, Level int
}

/** Index is an opened index directory. Files are read when a query needs them,
 * each checked against its digest. It is safe for concurrent use.
 */
type Index struct {
	src     Source
	m       manifest
	docOnly map[string]bool // words stored without positions (manifest documentOnly)

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
	ix, err := openSource(os.DirFS(dir), dir)
	if err != nil {
		return nil, err
	}
	return ix, nil
}

/** Source is where an index's files are read from: a file system whose root is
 * the index (the directory holding manifest.json). Names are slash-separated
 * and relative ("manifest.json", "w/<digest>.cbor.gz"); a missing file reports
 * an error satisfying errors.Is(err, fs.ErrNotExist). os.DirFS is the local
 * source; an HTTP source (a site's published index) implements the same
 * interface.
 *
 * Example:
 *   var src index.Source = os.DirFS("idx")
 */
type Source = fs.FS

/** OpenSource is Open for any Source: it reads and checks manifest.json.
 *
 * Parameters:
 *   src (Source) — the index files
 *
 * Returns:
 *   *Index — the opened index
 *   error  — ErrMissing when there is no manifest; ErrData when it is wrong
 *
 * Example:
 *   ix, err := index.OpenSource(os.DirFS("idx"))
 */
func OpenSource(src Source) (*Index, error) { return openSource(src, "the source") }

func openSource(src Source, where string) (*Index, error) {
	f, err := src.Open("manifest.json")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, wrapMissing(where)
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
	if err := checkDuplicateKeys(raw); err != nil {
		return nil, err
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
	var lacking []string
	hasDocOnly := false
	for _, f := range m.Required {
		if !knownFeatures[f] {
			lacking = append(lacking, f)
		}
		hasDocOnly = hasDocOnly || f == FeatureDocumentOnly
	}
	if len(lacking) > 0 {
		return nil, dataf("the index requires features this reader lacks: %s", strings.Join(lacking, ", "))
	}
	if len(m.DocumentOnly) > 0 && !hasDocOnly {
		return nil, dataf("the manifest lists document-only words but does not require %s", FeatureDocumentOnly)
	}
	docOnly := make(map[string]bool, len(m.DocumentOnly))
	for i, w := range m.DocumentOnly {
		if i > 0 && w <= m.DocumentOnly[i-1] {
			return nil, dataf("the document-only words are not in order")
		}
		docOnly[w] = true
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
	return &Index{src: src, m: m, docOnly: docOnly, chunks: map[int][]entry{}, frags: map[int]*fragmentRec{}}, nil
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

/** DocumentOnly returns the words the index stores without positions: the
 * pages they are on and the fields they are in, not where. A phrase through
 * one of them can match a page where the other words are in place.
 *
 * Returns:
 *   []string — the terms, sorted; nil when there are none
 *
 * Example:
 *   for _, w := range ix.DocumentOnly() { fmt.Println(w) }
 */
func (ix *Index) DocumentOnly() []string { return append([]string(nil), ix.m.DocumentOnly...) }

func (ix *Index) chunk(i int) ([]entry, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if e, ok := ix.chunks[i]; ok {
		return e, nil
	}
	raw, err := readFile(ix.src, "w", ix.m.Compression, ix.m.Chunks[i].File)
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
		raw, err := readFile(ix.src, "d", ix.m.Compression, r.File)
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
	ix.mu.Lock()
	if f, ok := ix.frags[doc]; ok {
		ix.mu.Unlock()
		return f, nil
	}
	ix.mu.Unlock()
	fr, err := ix.loadFragment(doc)
	if err != nil {
		return nil, err
	}
	ix.mu.Lock()
	ix.frags[doc] = fr
	ix.mu.Unlock()
	return fr, nil
}

// loadFragment reads and decodes the fragment of a document without keeping it:
// verification reads one for each candidate hit and the page may be megabytes.
func (ix *Index) loadFragment(doc int) (*fragmentRec, error) {
	rows, err := ix.docRows()
	if err != nil {
		return nil, err
	}
	if doc < 0 || doc >= len(rows) {
		return nil, dataf("no document %d", doc)
	}
	if len(rows[doc].Fragment) != 32 {
		return nil, dataf("document %d has a bad fragment digest", doc)
	}
	raw, err := readFile(ix.src, "f", ix.m.Compression, fileRef{Digest: hex.EncodeToString(rows[doc].Fragment)})
	if err != nil {
		return nil, err
	}
	var fr fragmentRec
	if err := decMode.Unmarshal(raw, &fr); err != nil {
		return nil, dataf("fragment of document %d: %v", doc, err)
	}
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
	for _, a := range fr.Anchors {
		out.Anchors = append(out.Anchors, Anchor{Slug: a.Slug, Heading: a.Heading, Position: int(a.Position), Level: int(a.Level)})
	}
	return out, nil
}

// termPostings is a document's weighted use of one word: positions with weights.
type use struct {
	pos    int
	weight int
}

// scan calls fn with every posting of the words matched by the term (an exact
// word or, with Prefix, every word with that prefix): the document number and
// the uses. A document-only word has one use per field it is in, with the
// unknown position -1.
func (ix *Index) scan(t Term, fn func(doc int, uses []use)) error {
	from := sort.Search(len(ix.m.Chunks), func(i int) bool { return ix.m.Chunks[i].To >= t.Text })
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
			return err
		}
		for _, e := range entries {
			if t.Prefix && !strings.HasPrefix(e.Word, t.Text) || !t.Prefix && e.Word != t.Text {
				continue
			}
			doc := 0
			only := ix.docOnly[e.Word]
			for _, p := range e.Postings {
				doc += int(p.Delta)
				if only {
					fn(doc, weightUses(p.Locations))
				} else {
					fn(doc, decodeLocations(p.Locations))
				}
			}
		}
	}
	return nil
}

// lookup returns, for each document, the uses of every word matched by the
// term.
func (ix *Index) lookup(t Term) (map[int][]use, error) {
	out := map[int][]use{}
	err := ix.scan(t, func(doc int, us []use) { out[doc] = append(out[doc], us...) })
	return out, err
}

// lookupDoc is lookup for one document.
func (ix *Index) lookupDoc(t Term, want int) (map[int][]use, error) {
	out := map[int][]use{}
	err := ix.scan(t, func(doc int, us []use) {
		if doc == want {
			out[doc] = append(out[doc], us...)
		}
	})
	return out, err
}

// unknownPos is the position of a use of a document-only word.
const unknownPos = -1

// weightUses reads the locations of a document-only word: the weights of the
// fields it occurs in, each a use at an unknown position.
func weightUses(ws []int64) []use {
	out := make([]use, 0, len(ws))
	for _, w := range ws {
		if w > 0 {
			out = append(out, use{pos: unknownPos, weight: int(w)})
		}
	}
	return out
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

/** Search runs a parsed query and returns up to limit hits, best first (ties
 * by document number). The score is the BM25 sum over the positive clauses a
 * page matched, with the writer's field weights; a NOT clause only filters. A
 * phrase matches consecutive words inside one field; a field clause counts
 * only that field's positions.
 *
 * Parameters:
 *   n (*Node)    — a tree from ParseQuery; nil matches nothing
 *   limit (int)  — most hits to return; 0 or less means no limit
 *
 * Returns:
 *   []Hit — matches, best first; empty (not an error) when nothing matches
 *   error — ErrData when a file does not match its digest or is malformed
 *
 * Example:
 *   n, _ := index.ParseQuery("soil -clay", nil)
 *   hits, err := ix.Search(n, 10)
 */
func (ix *Index) Search(n *Node, limit int) ([]Hit, error) {
	if n == nil {
		return nil, nil
	}
	rows, err := ix.docRows()
	if err != nil {
		return nil, err
	}
	total := float64(len(rows))
	avg := float64(ix.m.Documents.Words) / math.Max(total, 1)
	if avg == 0 {
		avg = 1
	}
	ev := &evaluator{ix: ix, rows: rows, n: total, avg: avg}
	scores, superset, err := ev.eval(n)
	if err != nil {
		return nil, err
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
	if superset {
		// The index's answer holds every page that matches and perhaps more
		// (a phrase through a document-only word): settle each hit on the
		// page's own words, best first, until there are enough.
		kept := make([]Hit, 0, min(len(hits), max(limit, 16)))
		for _, h := range hits {
			fr, err := ix.loadFragment(h.Doc)
			if err != nil {
				return nil, err
			}
			if !matchPage(n, fr) {
				continue
			}
			h.Address = fr.Address
			kept = append(kept, h)
			if limit > 0 && len(kept) >= limit {
				break
			}
		}
		return kept, nil
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	for i := range hits {
		// The address is in the document row; only an index written before
		// rows carried it needs the page's fragment.
		if d := hits[i].Doc; d >= 0 && d < len(rows) && rows[d].Address != "" {
			hits[i].Address = rows[d].Address
			continue
		}
		fr, err := ix.fragment(hits[i].Doc)
		if err != nil {
			return nil, err
		}
		hits[i].Address = fr.Address
	}
	return hits, nil
}

// fieldWeight maps a field to the weight of its segment; weights are unique, so
// a position's weight says which field it is in.
var fieldWeight = map[string]int{"title": weightTitle, "description": weightDescription, "body": weightBody}

type evaluator struct {
	ix   *Index
	rows []documentRow
	n    float64
	avg  float64
}

// eval returns the pages a node matches with their scores. The second result
// is true when the answer is only a superset of the pages that match: a phrase
// through a document-only word can hold pages that do not have the phrase, and
// whatever is built on it (AND, OR) is a superset too. NOT of a superset is
// every page, never its complement, which would drop true answers.
func (ev *evaluator) eval(n *Node) (map[int]float64, bool, error) {
	switch n.Kind {
	case KindTerm, KindPhrase:
		out, err := ev.leaf(n)
		return out, ev.ix.uncertain(n), err
	case KindOr:
		out := map[int]float64{}
		sup := false
		for _, k := range n.Kids {
			s, ksup, err := ev.eval(k)
			if err != nil {
				return nil, false, err
			}
			sup = sup || ksup
			for d, v := range s {
				out[d] += v
			}
		}
		return out, sup, nil
	case KindNot:
		s, sup, err := ev.eval(n.Kids[0])
		if err != nil {
			return nil, false, err
		}
		if sup {
			return ev.complement(map[int]float64{}), true, nil
		}
		return ev.complement(s), false, nil
	}
	// and: intersect the positive clauses, then remove what the NOT clauses match
	var out map[int]float64
	var negs []*Node
	sup := false
	for _, k := range n.Kids {
		if k.Kind == KindNot {
			negs = append(negs, k.Kids[0])
			continue
		}
		s, ksup, err := ev.eval(k)
		if err != nil {
			return nil, false, err
		}
		sup = sup || ksup
		if out == nil {
			out = s
			continue
		}
		next := map[int]float64{}
		for d, v := range out {
			if w, ok := s[d]; ok {
				next[d] = v + w
			}
		}
		out = next
	}
	if out == nil {
		out = ev.complement(map[int]float64{})
	}
	for _, k := range negs {
		s, ksup, err := ev.eval(k)
		if err != nil {
			return nil, false, err
		}
		if ksup {
			sup = true // what it matches is not known: remove nothing, check the page text
			continue
		}
		for d := range s {
			delete(out, d)
		}
	}
	return out, sup, nil
}

// uncertain reports whether the index's answer for a leaf is only a superset:
// a phrase with a document-only word, whose place in the page it does not know.
func (ix *Index) uncertain(n *Node) bool {
	return n.Kind == KindPhrase && ix.hasDocumentOnly(n)
}

// hasDocumentOnly reports whether any word of a term or phrase is stored
// without positions (the last word, when it is a prefix, if any word it starts
// is).
func (ix *Index) hasDocumentOnly(n *Node) bool {
	if len(ix.docOnly) == 0 {
		return false
	}
	for i, w := range n.Words {
		if n.Prefix && i == len(n.Words)-1 {
			for _, d := range ix.m.DocumentOnly {
				if strings.HasPrefix(d, w) {
					return true
				}
			}
		} else if ix.docOnly[w] {
			return true
		}
	}
	return false
}

func (ev *evaluator) complement(s map[int]float64) map[int]float64 {
	out := make(map[int]float64, len(ev.rows))
	for d := range ev.rows {
		if _, in := s[d]; !in {
			out[d] = 0
		}
	}
	return out
}

// filtered keeps the uses in the node's field (all of them when it has none).
func filtered(uses map[int][]use, field string) map[int][]use {
	w, ok := fieldWeight[field]
	if field == "" {
		return uses
	}
	out := map[int][]use{}
	if !ok {
		return out // a field this index has no segment for
	}
	for d, us := range uses {
		for _, u := range us {
			if u.weight == w {
				out[d] = append(out[d], u)
			}
		}
	}
	return out
}

func (ev *evaluator) leaf(n *Node) (map[int]float64, error) {
	sets := make([]map[int][]use, len(n.Words))
	for i, w := range n.Words {
		u, err := ev.ix.lookup(Term{Text: w, Prefix: n.Prefix && i == len(n.Words)-1})
		if err != nil {
			return nil, err
		}
		sets[i] = filtered(u, n.Field)
	}
	docs := map[int]bool{}
	for d := range sets[0] {
		docs[d] = true
	}
	for _, s := range sets[1:] {
		for d := range docs {
			if _, ok := s[d]; !ok {
				delete(docs, d)
			}
		}
	}
	out := map[int]float64{}
	for d := range docs {
		if len(sets) > 1 && !phraseAt(sets, d) {
			continue
		}
		var score float64
		for _, s := range sets {
			score += bm25(s[d], len(s), ev.n, float64(ev.rows[d].WordCount), ev.avg)
		}
		out[d] = score
	}
	return out, nil
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

// phraseAt reports whether the words occur at consecutive positions of
// document d inside one field: equal weights on consecutive positions mean the
// same segment, because each segment has its own weight. A word with no known
// position (document-only) matches wherever the document has it in that field;
// the caller (Search) settles such a phrase on the page text.
func phraseAt(sets []map[int][]use, d int) bool { return len(phraseStarts(sets, d)) > 0 }

// phraseStarts returns the places where the phrase starts in document d: a use
// whose pos is where the first word is and whose weight is the field. When no
// word of the phrase has a known position the page matches if every word is in
// one field, and the use has pos unknownPos.
//
// The word with the fewest known places proposes the starts and each is
// checked against the others by binary search, so a phrase of very common
// words costs the places of its rarest word, not a scan of the page for each.
func phraseStarts(sets []map[int][]use, d int) []use {
	type word struct {
		known   []use        // sorted by position
		unknown map[int]bool // fields the word is in at an unknown place
	}
	words := make([]word, len(sets))
	rarest := -1
	for k := range sets {
		w := word{unknown: map[int]bool{}}
		for _, u := range sets[k][d] {
			if u.pos == unknownPos {
				w.unknown[u.weight] = true
			} else {
				w.known = append(w.known, u)
			}
		}
		if len(w.known) == 0 && len(w.unknown) == 0 {
			return nil // a word the page does not have
		}
		sort.Slice(w.known, func(i, j int) bool { return w.known[i].pos < w.known[j].pos })
		words[k] = w
		if len(w.known) > 0 && (rarest < 0 || len(w.known) < len(words[rarest].known)) {
			rarest = k
		}
	}
	has := func(w word, pos, weight int) bool {
		if w.unknown[weight] {
			return true
		}
		i := sort.Search(len(w.known), func(i int) bool { return w.known[i].pos >= pos })
		for ; i < len(w.known) && w.known[i].pos == pos; i++ {
			if w.known[i].weight == weight {
				return true
			}
		}
		return false
	}
	var out []use
	if rarest < 0 {
		// no known position anywhere: every word in one field is the best a page can show
		for weight := range words[0].unknown {
			ok := true
			for k := 1; k < len(words) && ok; k++ {
				ok = words[k].unknown[weight]
			}
			if ok {
				out = append(out, use{pos: unknownPos, weight: weight})
			}
		}
	} else {
		seen := map[use]bool{}
		for _, u := range words[rarest].known {
			start := use{pos: u.pos - rarest, weight: u.weight}
			if start.pos < 0 || seen[start] {
				continue
			}
			seen[start] = true
			ok := true
			for k := 0; k < len(words) && ok; k++ {
				ok = has(words[k], start.pos+k, start.weight)
			}
			if ok {
				out = append(out, start)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pos != out[j].pos {
			return out[i].pos < out[j].pos
		}
		return out[i].weight < out[j].weight
	})
	return out
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
	return verifyIndex(ix)
}

/** VerifySource is Verify for any Source.
 *
 * Parameters:
 *   src (Source) — the index files
 *
 * Returns:
 *   error — nil when sound; ErrMissing without a manifest; ErrData otherwise
 *
 * Example:
 *   err := index.VerifySource(os.DirFS("idx"))
 */
func VerifySource(src Source) error {
	ix, err := OpenSource(src)
	if err != nil {
		return err
	}
	return verifyIndex(ix)
}

func verifyIndex(ix *Index) error {
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
		if r.Address != "" && r.Address != fr.Address {
			return dataf("document %d: the table says %s, its fragment is of %s", d, r.Address, fr.Address)
		}
	}
	return nil
}

/** Term is one word of a lookup: an exact word, or with Prefix every word that
 * starts with Text.
 */
type Term struct {
	Text   string
	Prefix bool
}

// checkDuplicateKeys refuses JSON in which an object names a key twice:
// encoding/json keeps the last one silently, so two readers could disagree
// about which value counts.
func checkDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []map[string]bool // one set per open object; nil for an array
	var isKey []bool           // whether the next string in the container is a key
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return dataf("manifest.json: %v", err)
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				keys = append(keys, map[string]bool{})
				isKey = append(isKey, true)
			case '[':
				keys = append(keys, nil)
				isKey = append(isKey, false)
			default:
				keys, isKey = keys[:len(keys)-1], isKey[:len(isKey)-1]
				if n := len(keys); n > 0 && keys[n-1] != nil {
					isKey[n-1] = true // a value ended; the next token is a key
				}
			}
			continue
		case string:
			if n := len(keys); n > 0 && keys[n-1] != nil && isKey[n-1] {
				if keys[n-1][v] {
					return dataf("manifest.json has a duplicate key %q", v)
				}
				keys[n-1][v] = true
				isKey[n-1] = false
				continue
			}
		}
		// a scalar value
		if n := len(keys); n > 0 && keys[n-1] != nil {
			isKey[n-1] = true
		}
	}
}
