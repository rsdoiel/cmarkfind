package index

import (
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// parse is the tree's compact form for tests: words, "phrases", prefix*,
// field:word, (and ...), (or ...), (not x); nil prints as "nil".
func parse(t *testing.T, q string) string {
	t.Helper()
	n, err := ParseQuery(q, nil)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", q, err)
	}
	return n.String()
}

func TestParseQueryTable(t *testing.T) {
	cases := []struct{ in, want string }{
		// words, phrases, prefix (rules 5 and 6)
		{"go", "go"},
		{"GO", "go"},
		{"go modules", "(and go modules)"},
		{"a b c", "(and a b c)"},
		{`"static site"`, `"static site"`},
		{`"Go Modules"`, `"go modules"`},
		{`"go"`, "go"},
		{"mark*", "mark*"},
		{"Go*", "go*"},
		{"a* b*", "(and a* b*)"},
		{"ma*rk", "(and ma* rk)"},
		{"*mark", "mark"},
		{"*", "nil"},
		{`"mark*"`, "mark"},
		{"state-of-the-art", `"state of the art"`},
		{"don't", `"don t"`},
		{"state-of*", `"state of*"`},
		{"c++", "c"},
		{"wow!", "wow"},
		{"a&&b", `"a b"`},
		// nothing searchable
		{"", "nil"}, {"   ", "nil"}, {"---", "nil"}, {"&", "nil"}, {"+", "nil"}, {"-", "nil"}, {"()", "nil"},
		// operators and precedence (rules 3 to 5)
		{"go OR rust", "(or go rust)"},
		{"go AND rust", "(and go rust)"},
		{"go && rust", "(and go rust)"},
		{"go || rust", "(or go rust)"},
		{"a OR b OR c", "(or a b c)"},
		{"go rust OR zig", "(or (and go rust) zig)"},
		{"a OR b AND c", "(or a (and b c))"},
		{"(a OR b) c", "(and (or a b) c)"},
		{"((a))", "a"},
		{"(a) (b)", "(and a b)"},
		{"a ()", "a"},
		{"go -draft", "(and go (not draft))"},
		{"go NOT draft", "(and go (not draft))"},
		{"go !draft", "(and go (not draft))"},
		{"!draft", "(not draft)"},
		{"+go +rust", "(and go rust)"},
		{"NOT a b", "(and (not a) b)"},
		{"NOT NOT a", "(not (not a))"},
		{"-a -b", "(and (not a) (not b))"},
		{"(-a)", "(not a)"},
		{"a OR NOT b", "(or a (not b))"},
		{"a-b", `"a b"`},
		{"a -b", "(and a (not b))"},
		// fields (rule 7)
		{"title:go", "title:go"},
		{"Title:go", "title:go"},
		{"BODY:go", "body:go"},
		{`title:"static site"`, `title:"static site"`},
		{"title:(a b)", "(and title:a title:b)"},
		{"title:(a OR b) c", "(and (or title:a title:b) c)"},
		{"title:(a body:b)", "(and title:a body:b)"},
		{"-title:a", "(not title:a)"},
		{"description:go*", "description:go*"},
		// unknown names and wrong roles are text, never errors
		{"note:this", "(and note this)"},
		{"https://example.org", `(and https "example org")`},
		{"title:", "title"},
		{"title: go", "(and title go)"},
		{"date:[2026 TO 2026]", "(and date 2026 to 2026)"}, // date is not a field until Phase 6
		{"title:[a TO b]", "(and title a to b)"},
		// malformed input (rule 9)
		{`"unclosed phrase`, "(and unclosed phrase)"},
		{`a "b`, "(and a b)"},
		{"(a b", "(and a b)"},
		{"a b)", "(and a b)"},
		{"a ) ( b", "(and a b)"},
		{"[a b", "(and a b)"},
		{"a]", "a"},
		{"{x}", "x"},
		{"AND a", "(and and a)"},
		{"a AND", "(and a and)"},
		{"a OR", "(and a or)"},
		{"a NOT", "(and a not)"},
		{"OR", "or"},
		{"a AND AND b", "(and a and b)"},
		{"a && ", "a"},
		{"|| a", "a"},
		{"a and b", "(and a and b)"},
		{"a or b", "(and a or b)"},
		{"a - b", "(and a b)"},
		{"a -", "a"},
		{`\"quoted\"`, "quoted"},
		{`title\:go`, `"title go"`},
		{`\(a\)`, "a"},
		{`a \* b`, "(and a b)"},
	}
	for _, c := range cases {
		if got := parse(t, c.in); got != c.want {
			t.Errorf("%q\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}

func TestParseQueryKnownFields(t *testing.T) {
	// the index set's mapping decides which names are fields
	n, err := ParseQuery("author:doiel title:go", []string{"author"})
	if err != nil {
		t.Fatal(err)
	}
	if got := n.String(); got != "(and author:doiel title go)" {
		t.Fatalf("got %s", got)
	}
}

func TestParseQueryLimits(t *testing.T) {
	ok := []string{
		strings.Repeat("a", 1024),
		strings.TrimSpace(strings.Repeat("w ", 64)),
		strings.Repeat("(", 8) + "a" + strings.Repeat(")", 8),
	}
	for _, q := range ok {
		if _, err := ParseQuery(q, nil); err != nil {
			t.Errorf("%.20q... within limits but: %v", q, err)
		}
	}
	bad := []string{
		strings.Repeat("a", 1025),
		strings.TrimSpace(strings.Repeat("w ", 65)),
		strings.Repeat("(", 9) + "a" + strings.Repeat(")", 9),
	}
	for _, q := range bad {
		_, err := ParseQuery(q, nil)
		if err == nil || !IsQueryLimit(err) {
			t.Errorf("%.20q... over a limit: err = %v", q, err)
		}
	}
}

// The parser is total: whatever the input, no panic and no error but a limit.
func TestParseQueryIsTotal(t *testing.T) {
	alphabet := []string{"a", "b", "title:", "body:", "AND", "OR", "NOT", "TO", "&&", "||", "!", "+", "-", `"`, "(", ")", "[", "]", "{", "}", ":", `\`, "*", " ", " ", " ", "é", "\xff", "\x00", "~", "^", "/", "?"}
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 20000; i++ {
		var sb strings.Builder
		for j, n := 0, rng.Intn(14); j < n; j++ {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		q := sb.String()
		n, err := ParseQuery(q, nil)
		if err != nil && !IsQueryLimit(err) {
			t.Fatalf("%q: %v", q, err)
		}
		if err == nil {
			_ = n.String() // walking a nil or odd tree must not panic either
		}
	}
}

func TestQueryLimitIsAUsageClassError(t *testing.T) {
	_, err := ParseQuery(strings.Repeat("a", 2000), nil)
	if err == nil || !IsQueryLimit(err) || IsDataError(err) {
		t.Fatalf("err = %v", err)
	}
}

// ---- semantics against a brute-force oracle (rule 10)

type oracleDoc struct {
	addr string
	seg  map[string][]string // field -> words
}

func oracleDocs(docs []Doc) []oracleDoc {
	var out []oracleDoc
	for _, d := range docs {
		od := oracleDoc{addr: d.Address, seg: map[string][]string{}}
		for name, text := range map[string]string{"title": d.Title, "description": d.Description, "body": d.Body} {
			for _, w := range Words(MarkdownText(text)) {
				od.seg[name] = append(od.seg[name], w.Term)
			}
		}
		out = append(out, od)
	}
	return out
}

func (d oracleDoc) eval(n *Node, field string) bool {
	fields := []string{"title", "description", "body"}
	if n.Field != "" {
		field = n.Field
	}
	if field != "" {
		fields = []string{field}
	}
	switch n.Kind {
	case KindAnd:
		for _, k := range n.Kids {
			if !d.eval(k, field) {
				return false
			}
		}
		return true
	case KindOr:
		for _, k := range n.Kids {
			if d.eval(k, field) {
				return true
			}
		}
		return false
	case KindNot:
		return !d.eval(n.Kids[0], field)
	}
	for _, f := range fields { // term or phrase: inside one field
		ws := d.seg[f]
		for i := range ws {
			ok := i+len(n.Words) <= len(ws)
			for k := 0; ok && k < len(n.Words); k++ {
				last := k == len(n.Words)-1
				if last && n.Prefix {
					ok = strings.HasPrefix(ws[i+k], n.Words[k])
				} else {
					ok = ws[i+k] == n.Words[k]
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

var semanticQueries = []string{
	"tomatoes", "tomatoes seeds", "tomatoes OR fox", "tomatoes -seeds", "-tomatoes", "!tomatoes",
	"(tomatoes OR fox) -seeds", "tomatoes OR NOT seeds", "NOT NOT soil", "soil OR index OR fox",
	"title:search", "title:tomatoes", "body:tomatoes", "description:index", "description:soil body:soil",
	"title:(gardening OR search)", "-title:search", `"quick brown fox"`, `"brown quick"`, `title:"search engines"`,
	`"untitled nothing"`, "gard*", "tomato* -seeds", "title:gard*", "s*", "zebra", "zebra OR fox",
	"index words pages", `"words to pages"`, "the", "-the",
}

func TestSearchAgainstOracle(t *testing.T) {
	docs := fixture()
	ix := open(t, build(t, docs, 7))
	oracle := oracleDocs(docs)
	for _, q := range semanticQueries {
		n, err := ParseQuery(q, nil)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := ix.Search(n, 0)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		var want []string
		if n != nil {
			for _, d := range oracle {
				if d.eval(n, "") {
					want = append(want, d.addr)
				}
			}
		}
		got := addrs(hits)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q: index %v, oracle %v", q, got, want)
		}
	}
}

func TestNotAddsNothingToTheScore(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	a, _ := search(ix, "tomatoes", 0)
	b, _ := search(ix, "tomatoes -fox", 0)
	if len(a) != len(b) {
		t.Fatalf("filter changed the set: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Address != b[i].Address || a[i].Score != b[i].Score {
			t.Fatalf("NOT changed ranking: %+v vs %+v", a[i], b[i])
		}
	}
}

func TestEmptyQueryFindsNothing(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	for _, q := range []string{"", "---", "()"} {
		hits, err := search(ix, q, 10)
		if err != nil || len(hits) != 0 {
			t.Errorf("%q: %v %v", q, hits, err)
		}
	}
}

func TestPhraseInFieldAndCrossFieldFieldClause(t *testing.T) {
	ix := open(t, build(t, fixture(), 0))
	hits, _ := search(ix, `title:"search engines"`, 10)
	if !reflect.DeepEqual(addrs(hits), []string{"https://example.org/a.md"}) {
		t.Fatalf("title phrase = %v", addrs(hits))
	}
	hits, _ = search(ix, `body:"search engines"`, 10)
	if len(hits) != 0 {
		t.Fatalf("body phrase matched a title: %v", addrs(hits))
	}
}
