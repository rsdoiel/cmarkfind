package index

import "strings"

// Exact matching of a query against one page's own words. A fragment holds
// the page's words in order and the sizes of its title, description and body,
// so a query can be answered on it without the index: the index's answer for a
// clause that has a document-only word is only a superset, and the page text
// settles it.

// pageWords is a page's words as terms, with the field each is in.
type pageWords struct {
	terms []string // the term of each word; "" for a word too long to be indexed
	field []string // "title", "description" or "body" for each word
	frag  *fragmentRec
}

var fieldOfWeight = map[int]string{weightTitle: "title", weightDescription: "description", weightBody: "body"}

// wordsOf reads a fragment's words. A word over MaxTermBytes has the term "",
// which no query word equals, as the index has no entry for it.
func wordsOf(fr *fragmentRec) pageWords {
	var surfaces []string
	if fr.Text != "" {
		surfaces = strings.Split(fr.Text, " ")
	}
	pw := pageWords{terms: make([]string, len(surfaces)), field: make([]string, len(surfaces)), frag: fr}
	for i, s := range surfaces {
		if t := term(s); len(t) <= MaxTermBytes {
			pw.terms[i] = t
		}
	}
	at := 0
	for _, seg := range fr.Segments {
		name := fieldOfWeight[int(seg[0])]
		for k := 0; k < int(seg[1]) && at < len(surfaces); k++ {
			pw.field[at] = name
			at++
		}
	}
	for ; at < len(surfaces); at++ { // a fragment without segments: all body
		pw.field[at] = "body"
	}
	return pw
}

// leafMatches returns the start positions of the term or phrase n in the page,
// each phrase inside one field and, when the node has a field, inside that one.
func (pw pageWords) leafMatches(n *Node, field string) []int {
	if n.Field != "" {
		field = n.Field
	}
	var out []int
	for i := 0; i+len(n.Words) <= len(pw.terms); i++ {
		if field != "" && pw.field[i] != field {
			continue
		}
		ok := true
		for k := 0; ok && k < len(n.Words); k++ {
			w := pw.terms[i+k]
			if w == "" || pw.field[i+k] != pw.field[i] {
				ok = false
			} else if n.Prefix && k == len(n.Words)-1 {
				ok = strings.HasPrefix(w, n.Words[k])
			} else {
				ok = w == n.Words[k]
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

func (pw pageWords) match(n *Node, field string) bool {
	if n.Field != "" {
		field = n.Field
	}
	switch n.Kind {
	case KindAnd:
		for _, k := range n.Kids {
			if !pw.match(k, field) {
				return false
			}
		}
		return true
	case KindOr:
		for _, k := range n.Kids {
			if pw.match(k, field) {
				return true
			}
		}
		return false
	case KindNot:
		return !pw.match(n.Kids[0], field)
	}
	if n.Kind == KindRange {
		return rangeMatches(pw.frag, n)
	}
	if !isTextField(n.Field) {
		return fieldMatches(pw.frag, n) // a front matter field: the values the fragment keeps
	}
	return len(pw.leafMatches(n, field)) > 0
}

// matchPage reports whether the query tree n matches the page of fragment fr,
// by the page's own words: the exact answer for every clause.
func matchPage(n *Node, fr *fragmentRec) bool {
	if n == nil {
		return false
	}
	return wordsOf(fr).match(n, "")
}
