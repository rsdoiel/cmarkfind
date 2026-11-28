package index

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTokenizeLowercasesAndSplitsOnEverythingThatIsNotALetterMarkOrDigit(t *testing.T) {
	cases := map[string][]string{
		"Hello, World!":     {"hello", "world"},
		"state-of-the-art":  {"state", "of", "the", "art"},
		"naïve CAFÉ":        {"naïve", "café"},
		"v0.0.1":            {"v0", "0", "1"},
		"snake_case":        {"snake", "case"},
		"e\u0301cole":       {"\u00e9cole"},                           // a combining mark stays in its word; the term is composed
		"日本語のテキスト":          {"日", "本", "語", "の", "テ", "キ", "ス", "ト"}, // one word per CJK character
		"don't":             {"don", "t"},
		"":                  nil,
		"  \t\n ":           nil,
		"!!! ??? ---":       nil,
		"Ünï Ünï":           {"ünï", "ünï"},
		"tab\tand\nnewline": {"tab", "and", "newline"},
	}
	for in, want := range cases {
		var got []string
		for _, w := range Words(in) {
			got = append(got, w.Term)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestWordsKeepTheSurfaceFormBesideTheTerm(t *testing.T) {
	w := Words("Hello, WORLD")
	if len(w) != 2 || w[0].Surface != "Hello" || w[0].Term != "hello" || w[1].Surface != "WORLD" || w[1].Term != "world" {
		t.Errorf("words %+v", w)
	}
}

func TestMarkdownTextRemovesSyntaxAndKeepsTheWords(t *testing.T) {
	src := "# A Title\n\nSome *emphasis*, **strong** and [a link](https://example.org/x?y=1) with `code`.\n\n" +
		"![alt words](img.png)\n\n- item one\n- item two\n\n> quoted text\n\n```go\nfunc Foo() {}\n```\n\n<div>raw html</div>\n\n| a | b |\n|---|---|\n| cell | cell2 |\n"
	got := strings.Fields(FragmentText(src))
	want := strings.Fields("A Title Some emphasis strong and a link with code alt words item one item two quoted text func Foo cell cell2 a b")
	if !reflect.DeepEqual(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	for _, bad := range []string{"https", "example.org", "img.png", "```", "**", "div"} {
		if strings.Contains(MarkdownText(src), bad) {
			t.Errorf("syntax or address %q survived: %q", bad, MarkdownText(src))
		}
	}
}

func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	for i := range c {
		for j := i + 1; j < len(c); j++ {
			if c[j] < c[i] {
				c[i], c[j] = c[j], c[i]
			}
		}
	}
	return c
}

// A fragment's text holds one surface word per token, joined by single spaces,
// so the whitespace split of the text has exactly as many words as the
// tokenizer found, and tokenizing the stored text again gives the same terms.
func checkInvariant(t *testing.T, name, markdown string) {
	t.Helper()
	text := FragmentText(markdown)
	words := Words(MarkdownText(markdown))
	if got := len(strings.Fields(text)); got != len(words) {
		t.Errorf("%s: whitespace split %d words, tokenizer %d", name, got, len(words))
		return
	}
	again := Words(text)
	if len(again) != len(words) {
		t.Errorf("%s: tokenizing the stored text gives %d words, first pass %d", name, len(again), len(words))
		return
	}
	for i := range words {
		if again[i].Term != words[i].Term {
			t.Errorf("%s: word %d is %q, then %q", name, i, words[i].Term, again[i].Term)
			return
		}
	}
}

func TestTheWhitespaceInvariantHoldsOnHardInputs(t *testing.T) {
	for name, src := range map[string]string{
		"hyphens":  "A state-of-the-art, well-known thing. v0.0.1 — done!",
		"unicode":  "日本語 テキスト naïve CAFÉ école ＦＵＬＬ ｗｉｄｔｈ",
		"spaces":   "no break em thin​zero",
		"empty":    "",
		"punct":    "!!! ??? ---",
		"code":     "```\nx := a-b\n```\n`y_z`",
		"nul-ctrl": "a\x00b\x01c\u0085d e",
	} {
		checkInvariant(t, name, src)
	}
}

// The repository's own Markdown is always checked; CMARKFIND_REAL_CORPUS names a
// folder of real pages for a wider run (set it to try the invariant on a corpus).
func TestTheWhitespaceInvariantHoldsOnRealMarkdown(t *testing.T) {
	roots := []string{".."}
	if r := os.Getenv("CMARKFIND_REAL_CORPUS"); r != "" {
		roots = append(roots, r)
	}
	n := 0
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			n++
			checkInvariant(t, p, string(b))
			return nil
		})
	}
	if n == 0 {
		t.Skip("no Markdown found")
	}
	t.Logf("checked %d files", n)
}

func TestTermsAreNormalizedToNFKCAndCaseFolded(t *testing.T) {
	same := [][]string{
		{"café", "café", "CAFÉ", "Café"}, // precomposed and decomposed
		{"fine", "ﬁne", "FINE"},            // ligature
		{"full", "ＦＵＬＬ", "Full"},           // full width
		{"strasse", "Straße", "STRASSE"},   // sharp s folds to ss
		{"οδοσ", "οδος", "ΟΔΟΣ"},           // Greek final sigma
		{"x2", "x²", "X₂"},                 // superscript and subscript digits
	}
	for _, group := range same {
		want := Words(group[0])
		if len(want) != 1 {
			t.Fatalf("%q is %d words", group[0], len(want))
		}
		for _, s := range group[1:] {
			got := Words(s)
			if len(got) != 1 || got[0].Term != want[0].Term {
				t.Errorf("%q: term %+v, want %q", s, got, want[0].Term)
			}
		}
	}
	// accents are not folded away: naïve and naive stay different words
	if Words("naïve")[0].Term == Words("naive")[0].Term {
		t.Error("an accent was folded away")
	}
}

func TestNormalizationKeepsTheSurfaceAndTheWordCount(t *testing.T) {
	in := "Café ﬁne ＦＵＬＬ Straße"
	ws := Words(in)
	if len(ws) != 4 {
		t.Fatalf("words %+v", ws)
	}
	for i, want := range []string{"Café", "ﬁne", "ＦＵＬＬ", "Straße"} {
		if ws[i].Surface != want {
			t.Errorf("surface %d: %q, want %q", i, ws[i].Surface, want)
		}
	}
}

func TestTheAnalyzerIsNamedV3(t *testing.T) {
	if Analyzer != "unicode-word-v3" {
		t.Errorf("analyzer %q", Analyzer)
	}
}

func TestTheASCIIFastPathGivesTheSameTermAsTheGeneralPath(t *testing.T) {
	for _, s := range []string{"Hello", "WORLD", "x1", "a", "MiXeD42", "v0"} {
		if got, want := term(s), cases.Fold().String(norm.NFKC.String(s)); got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}
