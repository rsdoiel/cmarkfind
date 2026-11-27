// Package index builds and reads the partitioned search index. This file is
// the text side: how a page's Markdown becomes words, the same way for the
// writer and every reader, so a stored word position means one word.
package index

import (
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Analyzer names the tokenizer below. A manifest carries it so a reader that
// does not implement it refuses the index instead of matching differently.
const Analyzer = "unicode-word-v1"

/** Word is one word of a page: what it looked like and what it is searched as.
 *
 * Fields:
 *   Surface (string) — the word as written (a run of letters, marks and numbers)
 *   Term (string)    — the surface form in lower case, the key of the index
 *
 * Example:
 *   w := index.Words("Hello, World")[0] // Surface "Hello", Term "hello"
 */
type Word struct {
	Surface, Term string
}

/** Words splits text into words by analyzer unicode-word-v1: a word is a
 * maximal run of Unicode letters, combining marks and numbers; everything else
 * (spaces, punctuation, symbols, the underscore and the apostrophe) ends one.
 * Terms are lower-cased with the standard library's case mapping. There is no
 * normalization, stemming or stop list, and scripts written without spaces are
 * one word per run. A later analyzer (for example one with NFKC and UAX 29
 * segmentation) takes another name.
 *
 * Parameters:
 *   s (string) — any text
 *
 * Returns:
 *   []Word — the words in order; nil when there are none
 *
 * Example:
 *   for _, w := range index.Words("state-of-the-art") { fmt.Println(w.Term) }
 */
func Words(s string) []Word {
	var out []Word
	start := -1
	for i, r := range s {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, newWord(s[start:i]))
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, newWord(s[start:]))
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r)
}

func newWord(surface string) Word { return Word{Surface: surface, Term: strings.ToLower(surface)} }

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

/** MarkdownText removes Markdown syntax and keeps the words: headings, list
 * and quote marks, emphasis, link and image addresses, raw HTML and code fences
 * go; link text, image alt text, code and table cell text stay. Blocks are
 * separated by a space.
 *
 * Parameters:
 *   src (string) — a page's Markdown (front matter already removed)
 *
 * Returns:
 *   string — the readable text
 *
 * Example:
 *   t := index.MarkdownText("# Hi\n\nSee [docs](https://x.org).") // "Hi See docs"
 */
func MarkdownText(src string) string {
	text, _ := markdownText(src)
	return text
}

// headingAt is a heading found by markdownText: its text, level and the byte
// offset in the text where it starts.
type headingAt struct {
	text   string
	level  int
	offset int
}

// markdownText is MarkdownText that also reports where the headings are.
func markdownText(src string) (string, []headingAt) {
	var heads []headingAt
	var open []int
	b := []byte(src)
	root := md.Parser().Parse(text.NewReader(b))
	var sb strings.Builder
	space := func() {
		if sb.Len() > 0 {
			last := sb.String()[sb.Len()-1]
			if last != ' ' {
				sb.WriteByte(' ')
			}
		}
	}
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch v := n.(type) {
		case *ast.Heading:
			if entering {
				space()
				heads = append(heads, headingAt{level: v.Level, offset: sb.Len()})
				open = append(open, len(heads)-1)
			} else {
				h := &heads[open[len(open)-1]]
				open = open[:len(open)-1]
				h.text = strings.TrimSpace(sb.String()[h.offset:])
				space()
			}
		case *ast.RawHTML, *ast.HTMLBlock:
			if entering {
				space()
			}
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if entering {
				sb.Write(v.Segment.Value(b))
				if v.SoftLineBreak() || v.HardLineBreak() {
					space()
				}
			}
		case *ast.String:
			if entering {
				sb.Write(v.Value)
			}
		case *ast.AutoLink:
			if entering {
				sb.Write(v.Label(b))
				space()
			}
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					seg := lines.At(i)
					sb.Write(seg.Value(b))
					space()
				}
			}
			return ast.WalkSkipChildren, nil
		default:
			if !entering && (n.Type() == ast.TypeBlock || n.Kind() == ast.KindLink) {
				space()
			}
			if entering && n.Kind().String() == "TableCell" {
				space()
			}
		}
		return ast.WalkContinue, nil
	})
	text := strings.TrimSpace(sb.String())
	return text, heads
}

/** FragmentText is the text a fragment stores: one surface word per word of the
 * page, joined by single spaces. The whitespace split of it has exactly as many
 * words as the tokenizer found, so a stored position indexes a word, and
 * tokenizing it again gives the same terms. Punctuation is not kept.
 *
 * Parameters:
 *   src (string) — a page's Markdown (front matter already removed)
 *
 * Returns:
 *   string — the words, space separated; empty for a page with none
 *
 * Example:
 *   t := index.FragmentText("# State-of-the-art\n") // "State of the art"
 */
func FragmentText(src string) string {
	words := Words(MarkdownText(src))
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = w.Surface
	}
	return strings.Join(parts, " ")
}
