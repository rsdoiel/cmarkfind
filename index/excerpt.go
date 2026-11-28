package index

import (
	"html"
	"sort"
	"strings"
)

// DefaultExcerptWords is the length of an excerpt window when none is given.
const DefaultExcerptWords = 30

/** Mark is one hit word of a page: its position in the fragment text and the
 * weight of the field it is in (title 4, description 2, body 1).
 */
type Mark struct {
	Pos, Weight int
}

/** Excerpt is a short piece of a page around its hits.
 *
 * Fields:
 *   Start, End (int)      — the window: words Start to End-1 of the fragment text
 *   Text (string)         — the window's words, plain
 *   HTML (string)         — the same, HTML-escaped, each hit word in <mark>
 *   Anchor (string)       — slug of the heading the first hit is under; "" before the first
 *   Leading, Trailing (bool) — the page has more words before / after the window
 *
 * Example:
 *   e := index.MakeExcerpt(f, marks, 30)
 */
type Excerpt struct {
	Start, End        int
	Text, HTML        string
	Anchor            string
	Leading, Trailing bool
}

/** MakeExcerpt picks the window of length words whose hits have the highest
 * summed weights (the first such window on a tie; the top of the page without
 * hits) and renders it. Fragment text is untrusted, so HTML is escaped.
 *
 * Parameters:
 *   f (Fragment)    — the stored page
 *   marks ([]Mark)  — hit words, as from Index.Marks
 *   length (int)    — window size in words; 0 or less means DefaultExcerptWords
 *
 * Returns:
 *   Excerpt — the window; zero for a page without words
 *
 * Example:
 *   e := index.MakeExcerpt(f, marks, 0)
 */
func MakeExcerpt(f Fragment, marks []Mark, length int) Excerpt {
	if length <= 0 {
		length = DefaultExcerptWords
	}
	words := strings.Fields(f.Text)
	n := len(words)
	if n == 0 {
		return Excerpt{}
	}
	ms := make([]Mark, 0, len(marks))
	for _, m := range marks {
		if m.Pos >= 0 && m.Pos < n {
			ms = append(ms, m)
		}
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].Pos < ms[j].Pos })
	if len(ms) > 0 && startsCJK(words[ms[0].Pos]) {
		length *= 3 // a CJK word is one character
	}
	length = min(length, n)
	start, best := 0, -1
	prefix := make([]int, len(ms)+1)
	for i, m := range ms {
		prefix[i+1] = prefix[i] + m.Weight
	}
	for _, m := range ms {
		s := min(max(m.Pos-length/6, 0), n-length)
		lo := sort.Search(len(ms), func(i int) bool { return ms[i].Pos >= s })
		hi := sort.Search(len(ms), func(i int) bool { return ms[i].Pos >= s+length })
		if sum := prefix[hi] - prefix[lo]; sum > best {
			best, start = sum, s
		}
	}
	end := start + length
	hit := map[int]bool{}
	first := -1
	for _, m := range ms {
		if m.Pos >= start && m.Pos < end {
			hit[m.Pos] = true
			if first < 0 {
				first = m.Pos
			}
		}
	}
	if first < 0 {
		first = start
	}
	var h strings.Builder
	for i := start; i < end; i++ {
		if i > start && SpaceBetween(words[i-1], words[i]) {
			h.WriteByte(' ')
		}
		w := html.EscapeString(words[i])
		if hit[i] {
			w = "<mark>" + w + "</mark>"
		}
		h.WriteString(w)
	}
	var plain strings.Builder
	for i := start; i < end; i++ {
		if i > start && SpaceBetween(words[i-1], words[i]) {
			plain.WriteByte(' ')
		}
		plain.WriteString(words[i])
	}
	e := Excerpt{Start: start, End: end, Text: plain.String(), HTML: h.String(),
		Leading: start > 0, Trailing: end < n}
	for _, a := range f.Anchors {
		if a.Position <= first {
			e.Anchor = a.Slug
		}
	}
	return e
}

/** Marks returns the words of document doc that the query's positive clauses
 * matched: every word of a term (a prefix matches every word it starts), the
 * words of a phrase where the phrase occurs, only inside the field of a field
 * clause. A NOT clause marks nothing. Sorted by position, one per word.
 *
 * Parameters:
 *   n (*Node)  — a tree from ParseQuery
 *   doc (int)  — a document number from a Hit
 *
 * Returns:
 *   []Mark — the hit words; empty when the page has none
 *   error  — ErrData when a file does not match its digest or is malformed
 *
 * Example:
 *   marks, err := ix.Marks(n, hit.Doc)
 */
func (ix *Index) Marks(n *Node, doc int) ([]Mark, error) {
	at := map[int]int{}
	var walk func(*Node) error
	walk = func(n *Node) error {
		switch n.Kind {
		case KindNot:
			return nil
		case KindRange:
			return nil // a date clause selects pages; there is no word of the text to mark
		case KindTerm, KindPhrase:
			if !isTextField(n.Field) {
				return nil // a front matter clause selects pages; there is no word of the text to mark
			}
			if ix.hasDocumentOnly(n) {
				// where the index has no positions the page's own words say
				fr, err := ix.fragment(doc)
				if err != nil {
					return err
				}
				pw := wordsOf(fr)
				for _, start := range pw.leafMatches(n, "") {
					for k := range n.Words {
						at[start+k] = max(at[start+k], fieldWeight[pw.field[start]])
					}
				}
				return nil
			}
			sets := make([]map[int][]use, len(n.Words))
			for i, w := range n.Words {
				u, err := ix.lookupDoc(Term{Text: w, Prefix: n.Prefix && i == len(n.Words)-1}, doc)
				if err != nil {
					return err
				}
				sets[i] = filtered(u, n.Field)
			}
			if len(sets) == 1 {
				for _, u := range sets[0][doc] {
					if u.pos != unknownPos {
						at[u.pos] = max(at[u.pos], u.weight)
					}
				}
				return nil
			}
			for _, u := range phraseStarts(sets, doc) {
				if u.pos == unknownPos {
					continue
				}
				for k := range sets {
					// a document-only word has no place to mark
					for _, v := range sets[k][doc] {
						if v.pos == u.pos+k && v.weight == u.weight {
							at[v.pos] = max(at[v.pos], u.weight)
						}
					}
				}
			}
			return nil
		}
		for _, k := range n.Kids {
			if err := walk(k); err != nil {
				return err
			}
		}
		return nil
	}
	if n != nil {
		if err := walk(n); err != nil {
			return nil, err
		}
	}
	out := make([]Mark, 0, len(at))
	for p, w := range at {
		out = append(out, Mark{Pos: p, Weight: w})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out, nil
}

/** Excerpt is the excerpt of document doc for the query: its marks, then the
 * best window.
 *
 * Parameters:
 *   n (*Node)    — the parsed query
 *   doc (int)    — a document number from a Hit
 *   length (int) — window size in words; 0 or less means DefaultExcerptWords
 *
 * Returns:
 *   Excerpt — the window
 *   error   — ErrData when a file does not match its digest or is malformed
 *
 * Example:
 *   e, err := ix.Excerpt(n, hit.Doc, 0)
 */
func (ix *Index) Excerpt(n *Node, doc int, length int) (Excerpt, error) {
	f, err := ix.Fragment(doc)
	if err != nil {
		return Excerpt{}, err
	}
	marks, err := ix.Marks(n, doc)
	if err != nil {
		return Excerpt{}, err
	}
	return MakeExcerpt(f, marks, length), nil
}
