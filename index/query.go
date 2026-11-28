package index

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of a query (the limits rule of the query language): a longer or
// deeper query is refused, not truncated.
const (
	maxQueryBytes   = 1024
	maxQueryClauses = 64
	maxQueryDepth   = 8
)

// ErrQueryLimit marks a query over a limit. It is a usage error (exit 2), not
// a data error: nothing read was wrong, the question was too big.
var ErrQueryLimit = errors.New("query is too large")

/** IsQueryLimit reports whether err says a query was over a size limit: more
 * than 1,024 bytes, 64 clauses or 8 levels of parentheses. It is the only
 * error ParseQuery returns.
 *
 * Parameters:
 *   err (error) — any error
 *
 * Returns:
 *   bool — true for a query limit error
 *
 * Example:
 *   if index.IsQueryLimit(err) { os.Exit(2) }
 */
func IsQueryLimit(err error) bool { return errors.Is(err, ErrQueryLimit) }

/** Kind says what a Node is. */
type Kind int

// The kinds of query node.
const (
	KindTerm Kind = iota
	KindPhrase
	KindAnd
	KindOr
	KindNot
	KindRange // a date field between two day numbers
)

/** Node is one node of a parsed query.
 *
 * Fields:
 *   Kind (Kind)      — term, phrase, range, and, or, not
 *   Field (string)   — "title", "description" or "body" for a leaf; "" for any
 *   Words ([]string) — a term's one word, or a phrase's words, lower case
 *   Prefix (bool)    — the last word is a prefix (trailing `*`)
 *   Kids ([]*Node)   — the operands of and, or and not
 *   Lo, Hi (int)     — a range's first and last day numbers, openLow and openHigh for no bound
 *
 * Example:
 *   n, _ := index.ParseQuery(`go -draft`, nil) // (and go (not draft))
 */
type Node struct {
	Kind   Kind
	Field  string
	Words  []string
	Prefix bool
	Kids   []*Node
	Lo, Hi int // KindRange: first and last day number wanted (inclusive); openLow and openHigh when there is no bound
}

/** String writes the tree in a compact form: `go`, `title:go`, `mark*`,
 * `"static site"`, `(and a b)`, `(or a b)`, `(not a)`; a nil node is `nil`.
 *
 * Returns:
 *   string — the compact form, for tests and diagnostics
 *
 * Example:
 *   fmt.Println(n.String())
 */
func (n *Node) String() string {
	if n == nil {
		return "nil"
	}
	switch n.Kind {
	case KindAnd, KindOr, KindNot:
		name := map[Kind]string{KindAnd: "and", KindOr: "or", KindNot: "not"}[n.Kind]
		parts := []string{name}
		for _, k := range n.Kids {
			parts = append(parts, k.String())
		}
		return "(" + strings.Join(parts, " ") + ")"
	}
	if n.Kind == KindRange {
		return n.Field + ":[" + dayText(n.Lo, openLow) + " TO " + dayText(n.Hi, openHigh) + "]"
	}
	s := strings.Join(n.Words, " ")
	if n.Prefix {
		s += "*"
	}
	if n.Kind == KindPhrase {
		s = `"` + s + `"`
	}
	if n.Field != "" {
		s = n.Field + ":" + s
	}
	return s
}

// defaultFields are the fields every index has: the three text segments of a page.
var defaultFields = []string{"title", "description", "body"}

type tokKind int

const (
	tTerm   tokKind = iota // text, star
	tQuoted                // text
	tLParen
	tRParen
	tField // text = lower-case name
	tSign  // text = "+", "-" or "!"
	tAnd
	tOr
	tNot
	tRange // text = the date field; lo and hi the day numbers
)

type token struct {
	kind   tokKind
	text   string
	star   bool
	lo, hi int
}

/** ParseQuery reads a query string into a tree by the grammar and rules of the
 * query language: Lucene query string syntax, restricted. It is total: malformed
 * input is read as text and never an error. The only error is a query over a
 * limit (IsQueryLimit). A query with nothing searchable gives a nil node.
 *
 * Parameters:
 *   s (string)        — what the person typed
 *   fields ([]string) — the field names the index knows; nil means title,
 *                       description and body. Names are matched without case.
 *
 * Returns:
 *   *Node  — the tree, or nil when there is no searchable word
 *   error  — ErrQueryLimit when the query is too large
 *
 * Example:
 *   n, err := index.ParseQuery(`title:go (rust OR zig) -draft`, nil)
 */
func ParseQuery(s string, fields []string) (*Node, error) { return ParseQueryFields(s, fields, nil) }

/** ParseQueryFields is ParseQuery for an index (or the union of several) that has
 * front matter fields. fields are the names a clause may name (title, description,
 * body and the fielded fields); dates are the date fields, on which a clause
 * `field:[2026-01 TO 2026-06]`, `field:{2025 TO 2027}` (`[` and `]` include a
 * bound, `{` and `}` exclude it, `*` is no bound) and `field:2026-06` are read as
 * a range of days. A partial date is a period: a bound includes or excludes the
 * whole period, and a date as a term is the period. A range or date that cannot
 * be read is read as words, like every malformed clause.
 *
 * Parameters:
 *   s (string)        — what the person typed
 *   fields ([]string) — the field names a clause may name; nil means title, description and body
 *   dates ([]string)  — the fields among them that are dates
 *
 * Returns:
 *   *Node — the tree, or nil when there is no searchable word
 *   error — ErrQueryLimit when the query is too large
 *
 * Example:
 *   n, err := index.ParseQueryFields(`created:[2026 TO 2027} AND go`, ix.QueryFields(), ix.DateFields())
 */
func ParseQueryFields(s string, fields, dates []string) (*Node, error) {
	if len(s) > maxQueryBytes {
		return nil, fmt.Errorf("%w: over %d bytes", ErrQueryLimit, maxQueryBytes)
	}
	if fields == nil {
		fields = defaultFields
	}
	known := map[string]bool{}
	for _, f := range fields {
		known[strings.ToLower(f)] = true
	}
	dateSet := map[string]bool{}
	for _, f := range dates {
		dateSet[strings.ToLower(f)] = true
	}
	toks := lexQuery(s, known, dateSet)
	toks, err := matchParens(toks)
	if err != nil {
		return nil, err
	}
	clauses := 0
	for _, t := range toks {
		if t.kind == tTerm || t.kind == tQuoted {
			clauses++
		}
	}
	if clauses > maxQueryClauses {
		return nil, fmt.Errorf("%w: over %d clauses", ErrQueryLimit, maxQueryClauses)
	}
	p := &parser{toks: resolveOperators(toks)}
	var parts []*Node
	for p.pos < len(p.toks) {
		before := p.pos
		parts = append(parts, p.parseOr())
		if p.pos == before {
			p.pos++ // a stray token no rule wants: skip it, never loop
		}
	}
	return and(parts), nil
}

func isTermChar(r rune) bool {
	if unicode.IsSpace(r) {
		return false
	}
	switch r {
	case '"', '(', ')', '[', ']', '{', '}', ':', '\\', '*':
		return false
	}
	return true
}

// startsPrimary reports whether r can begin the thing a field name applies to.
func startsPrimary(r rune) bool {
	if unicode.IsSpace(r) {
		return false
	}
	switch r {
	case ')', '[', ']', '{', '}', ':', '*':
		return false
	}
	return true
}

func lexQuery(s string, known, dates map[string]bool) []token {
	var toks []token
	i := 0
	rest := func() (rune, int) { return utf8.DecodeRuneInString(s[i:]) }
	for i < len(s) {
		r, w := rest()
		switch {
		case unicode.IsSpace(r):
			i += w
		case r == '(':
			toks = append(toks, token{kind: tLParen})
			i += w
		case r == ')':
			toks = append(toks, token{kind: tRParen})
			i += w
		case r == '"':
			text, end, ok := scanPhrase(s, i+w)
			if !ok {
				i += w // an unmatched quote is dropped
				continue
			}
			toks = append(toks, token{kind: tQuoted, text: text})
			i = end
		case r == '+' || r == '-' || r == '!':
			// a sign only at the start of a clause, attached to what follows
			if nr, _ := utf8.DecodeRuneInString(s[i+w:]); i+w < len(s) && startsPrimary(nr) {
				toks = append(toks, token{kind: tSign, text: string(r)})
				i += w
				continue
			}
			i = lexChunk(s, i, known, dates, &toks)
		case isTermChar(r) || r == '\\':
			i = lexChunk(s, i, known, dates, &toks)
		default: // [ ] { } : * stray: punctuation
			i += w
		}
	}
	return toks
}

// scanPhrase reads from just after an opening quote to the closing one.
func scanPhrase(s string, i int) (text string, end int, ok bool) {
	var sb strings.Builder
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '"':
			return sb.String(), i + w, true
		case '\\':
			i += w
			if i < len(s) {
				r2, w2 := utf8.DecodeRuneInString(s[i:])
				sb.WriteRune(r2)
				i += w2
			}
			continue
		}
		sb.WriteRune(r)
		i += w
	}
	return "", 0, false
}

// lexChunk reads a term chunk (with escapes) at i, then a trailing `*`, an
// operator word, or a field name before `:`; it appends one token and returns
// the new position.
func lexChunk(s string, i int, known, dates map[string]bool, toks *[]token) int {
	start := i
	var sb strings.Builder
	escaped := false
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == '\\' {
			escaped = true
			i += w
			if i < len(s) {
				r2, w2 := utf8.DecodeRuneInString(s[i:])
				sb.WriteRune(r2)
				i += w2
			}
			continue
		}
		if !isTermChar(r) {
			break
		}
		sb.WriteRune(r)
		i += w
	}
	text := sb.String()
	boundedBefore := start == 0 || isSpaceAt(s, start-1)
	if !escaped && boundedBefore && (i >= len(s) || isSpaceAt(s, i)) {
		kind := map[string]tokKind{"AND": tAnd, "&&": tAnd, "OR": tOr, "||": tOr, "NOT": tNot}
		if k, ok := kind[text]; ok {
			*toks = append(*toks, token{kind: k, text: text})
			return i
		}
	}
	if !escaped && i < len(s) && s[i] == ':' && dates[strings.ToLower(text)] && isFieldName(text) && i+1 < len(s) {
		if tok, end, ok := lexDate(s, i+1, strings.ToLower(text)); ok {
			*toks = append(*toks, tok)
			return end
		}
	}
	if !escaped && i < len(s) && s[i] == ':' && known[strings.ToLower(text)] && isFieldName(text) && i+1 < len(s) {
		if nr, _ := utf8.DecodeRuneInString(s[i+1:]); startsPrimary(nr) {
			*toks = append(*toks, token{kind: tField, text: strings.ToLower(text)})
			return i + 1
		}
	}
	star := false
	if i < len(s) && s[i] == '*' {
		star = true
		i++
	}
	*toks = append(*toks, token{kind: tTerm, text: text, star: star})
	return i
}

func isSpaceAt(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsSpace(r)
}

func isFieldName(s string) bool {
	for i, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r >= '0' && r <= '9' || r == '_')
		if !ok {
			return false
		}
	}
	return s != ""
}

// matchParens drops unmatched parentheses and enforces the depth limit.
func matchParens(toks []token) ([]token, error) {
	var stack []int
	drop := map[int]bool{}
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			stack = append(stack, i)
			if len(stack) > maxQueryDepth {
				return nil, fmt.Errorf("%w: more than %d levels of parentheses", ErrQueryLimit, maxQueryDepth)
			}
		case tRParen:
			if len(stack) == 0 {
				drop[i] = true
			} else {
				stack = stack[:len(stack)-1]
			}
		}
	}
	for _, i := range stack {
		drop[i] = true
	}
	out := toks[:0:0]
	for i, t := range toks {
		if !drop[i] {
			out = append(out, t)
		}
	}
	return out, nil
}

// operandStarts reports whether the token at i can begin an operand.
func operandStarts(toks []token, i int) bool {
	if i >= len(toks) {
		return false
	}
	switch toks[i].kind {
	case tTerm, tQuoted, tLParen, tField, tSign, tNot, tRange:
		return true
	}
	return false
}

// resolveOperators turns an operator word without the operands it needs into
// an ordinary word (AND, OR, NOT) or drops it (&&, ||).
func resolveOperators(toks []token) []token {
	var out []token
	for i, t := range toks {
		switch t.kind {
		case tAnd, tOr:
			left := len(out) > 0 && (out[len(out)-1].kind == tTerm || out[len(out)-1].kind == tQuoted || out[len(out)-1].kind == tRParen || out[len(out)-1].kind == tRange)
			if left && operandStarts(toks, i+1) {
				out = append(out, t)
			} else if t.text == "AND" || t.text == "OR" {
				out = append(out, token{kind: tTerm, text: strings.ToLower(t.text)})
			}
		case tNot:
			if operandStarts(toks, i+1) {
				out = append(out, t)
			} else {
				out = append(out, token{kind: tTerm, text: "not"})
			}
		default:
			out = append(out, t)
		}
	}
	return out
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() (token, bool) {
	if p.pos >= len(p.toks) {
		return token{}, false
	}
	return p.toks[p.pos], true
}

func (p *parser) parseOr() *Node {
	kids := []*Node{p.parseAnd()}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tOr {
			break
		}
		p.pos++
		kids = append(kids, p.parseAnd())
	}
	return or(kids)
}

func (p *parser) parseAnd() *Node {
	kids := []*Node{p.parseUnary()}
	for {
		t, ok := p.peek()
		if !ok {
			break
		}
		if t.kind == tAnd {
			p.pos++
		} else if t.kind != tTerm && t.kind != tQuoted && t.kind != tLParen && t.kind != tField && t.kind != tSign && t.kind != tNot {
			break
		}
		kids = append(kids, p.parseUnary())
	}
	return and(kids)
}

func (p *parser) parseUnary() *Node {
	t, ok := p.peek()
	if !ok {
		return nil
	}
	switch {
	case t.kind == tNot:
		p.pos++
		return not(p.parseUnary())
	case t.kind == tSign:
		p.pos++
		n := p.parseClause()
		if t.text == "+" {
			return n
		}
		return not(n)
	}
	return p.parseClause()
}

func (p *parser) parseClause() *Node {
	field := ""
	if t, ok := p.peek(); ok && t.kind == tField {
		field = t.text
		p.pos++
	}
	t, ok := p.peek()
	if !ok {
		return nil
	}
	var n *Node
	switch t.kind {
	case tTerm:
		p.pos++
		n = leaf(t.text, t.star)
		if n != nil && n.Kind == KindTerm && isHangulStem(n.Words[0]) {
			n.Prefix = true // a particle follows a Korean stem: 한국어 is found in 한국어를
		}
	case tQuoted:
		p.pos++
		n = leaf(t.text, false)
	case tRange:
		p.pos++
		return &Node{Kind: KindRange, Field: t.text, Lo: t.lo, Hi: t.hi}
	case tLParen:
		p.pos++
		n = p.parseOr()
		if t2, ok := p.peek(); ok && t2.kind == tRParen {
			p.pos++
		}
	default:
		return nil // an operator or sign with no clause after it: nothing here
	}
	if field != "" {
		setField(n, field)
	}
	return n
}

// isHangulStem says whether a query word is made only of Hangul syllables and
// has at least two of them: such a word is read as a prefix, since Korean
// attaches particles to the word they follow. One syllable is too short to be a
// stem, and a quoted word is never a prefix (it does not come through here).
func isHangulStem(word string) bool {
	n := 0
	for _, r := range word {
		if r < 0xAC00 || r > 0xD7A3 {
			return false
		}
		n++
	}
	return n >= 2
}

func setField(n *Node, field string) {
	if n == nil {
		return
	}
	if len(n.Kids) == 0 {
		if n.Field == "" {
			n.Field = field
		}
		return
	}
	for _, k := range n.Kids {
		setField(k, field)
	}
}

// leaf analyzes text into a term (one word) or a phrase (several); nil for none.
func leaf(text string, star bool) *Node {
	ws := Words(text)
	if len(ws) == 0 {
		return nil
	}
	words := make([]string, len(ws))
	for i, w := range ws {
		words[i] = w.Term
	}
	if len(words) == 1 {
		return &Node{Kind: KindTerm, Words: words, Prefix: star}
	}
	return &Node{Kind: KindPhrase, Words: words, Prefix: star}
}

func combine(kind Kind, kids []*Node) *Node {
	var flat []*Node
	for _, k := range kids {
		if k == nil {
			continue
		}
		if k.Kind == kind {
			flat = append(flat, k.Kids...)
		} else {
			flat = append(flat, k)
		}
	}
	switch len(flat) {
	case 0:
		return nil
	case 1:
		return flat[0]
	}
	return &Node{Kind: kind, Kids: flat}
}

func and(kids []*Node) *Node { return combine(KindAnd, kids) }
func or(kids []*Node) *Node  { return combine(KindOr, kids) }

func not(n *Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{Kind: KindNot, Kids: []*Node{n}}
}

// openLow and openHigh are the bounds of a range with no bound.
const (
	openLow  = math.MinInt
	openHigh = math.MaxInt
)

// dayText writes a day number as YYYY-MM-DD, or * for no bound.
func dayText(n, open int) string {
	if n == open {
		return "*"
	}
	return time.Unix(int64(n)*86400, 0).UTC().Format("2006-01-02")
}

// lexDate reads, at i (just after a date field's colon), a range `[A TO B]` or a
// date (bare or quoted), and returns the token and where it ends. A bound is a
// date or *. Anything else is not a date clause and the caller reads the text.
func lexDate(s string, i int, field string) (token, int, bool) {
	tok := token{kind: tRange, text: field, lo: openLow, hi: openHigh}
	switch s[i] {
	case '[', '{':
		end := strings.IndexAny(s[i+1:], "]}")
		if end < 0 {
			return tok, 0, false
		}
		inner := strings.Fields(s[i+1 : i+1+end])
		if len(inner) != 3 || !strings.EqualFold(inner[1], "TO") {
			return tok, 0, false
		}
		closing := s[i+1+end]
		if inner[0] != "*" {
			first, last, ok := ParseDay(inner[0])
			if !ok {
				return tok, 0, false
			}
			tok.lo = first
			if s[i] == '{' { // an exclusive bound drops its whole period
				tok.lo = last + 1
			}
		}
		if inner[2] != "*" {
			first, last, ok := ParseDay(inner[2])
			if !ok {
				return tok, 0, false
			}
			tok.hi = last
			if closing == '}' {
				tok.hi = first - 1
			}
		}
		return tok, i + 1 + end + 1, true
	case '"':
		text, end, ok := scanPhrase(s, i+1)
		if !ok {
			return tok, 0, false
		}
		first, last, ok := ParseDay(text)
		if !ok {
			return tok, 0, false
		}
		tok.lo, tok.hi = first, last
		return tok, end, true
	}
	j := i
	for j < len(s) && !isSpaceAt(s, j) && s[j] != ')' && s[j] != '(' && s[j] != '"' {
		j++
	}
	first, last, ok := ParseDay(s[i:j])
	if !ok {
		return tok, 0, false
	}
	tok.lo, tok.hi = first, last
	return tok, j, true
}
