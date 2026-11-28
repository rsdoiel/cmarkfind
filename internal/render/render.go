// Package render writes a search result as Markdown and as the JSON object the
// verbs print. The command line and the server both use it, so the server's
// Markdown is the CLI's bytes (cmarkfind DR-0018 item 3). Everything out of the
// corpus is other people's text: it is escaped here so it makes no markup.
package render

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

	"github.com/rsdoiel/cmarkfind/corpus"
)

/** Clean drops the control characters of stored text.
 *
 * Parameters:
 *   s (string) — text out of the corpus
 *
 * Returns:
 *   string — s without control characters
 *
 * Example:
 *   t := render.Clean("a\x07b") // "ab"
 */
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

/** Text escapes what would turn page text into Markdown markup, and drops
 * control characters.
 *
 * Parameters:
 *   s (string) — stored text
 *
 * Returns:
 *   string — Markdown that shows s as plain text
 *
 * Example:
 *   md := render.Text("[x](http://evil/)") // `\\[x\\](http://evil/)`
 */
func Text(s string) string {
	var b strings.Builder
	for _, r := range Clean(s) {
		switch r {
		case '\\', '[', ']', '*', '_', '`', '<', '>':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

/** Dest makes an address safe as the destination of an inline link: the
 * characters that would end or break it are percent-encoded.
 *
 * Parameters:
 *   s (string) — an address
 *
 * Returns:
 *   string — the address as a link destination
 *
 * Example:
 *   d := render.Dest("https://example.org/a (b).md") // "https://example.org/a%20%28b%29.md"
 */
func Dest(s string) string {
	var b strings.Builder
	for _, c := range []byte(Clean(s)) {
		switch {
		case c <= ' ', c == '(', c == ')', c == '<', c == '>', c == '\\', c == 0x7f:
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

/** Snippet turns a snippet's [matched] words into **bold** and escapes the rest.
 *
 * Parameters:
 *   s (string) — a snippet as the corpus makes it
 *
 * Returns:
 *   string — Markdown
 *
 * Example:
 *   md := render.Snippet("an [otter] swims") // "an **otter** swims"
 */
func Snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range Clean(s) {
		switch r {
		case '[', ']':
			b.WriteString("**")
		case '\\', '*', '_', '`', '<', '>':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

/** Hit writes one hit as a Markdown list item: a link, then an optional detail
 * line and an optional snippet, indented under it.
 *
 * Parameters:
 *   w (io.Writer)                     — where to write
 *   indent (string)                   — spaces before the item
 *   title, address, anchor (string)   — the page; an empty title shows the address
 *   detail, snippet (string)          — already Markdown (see Text and Snippet)
 *
 * Example:
 *   render.Hit(os.Stdout, "", "Otters", "https://example.org/a.md", "", "own", "")
 */
func Hit(w io.Writer, indent, title, address, anchor, detail, snippet string) {
	text := strings.TrimSpace(Clean(title))
	if text == "" {
		text = address
	}
	dest := Dest(address)
	if anchor != "" {
		dest += "#" + Dest(anchor)
	}
	fmt.Fprintf(w, "%s- [%s](%s)\n", indent, Text(text), dest)
	if detail != "" {
		fmt.Fprintf(w, "%s  %s\n", indent, detail)
	}
	if snippet != "" {
		fmt.Fprintf(w, "%s  %s\n", indent, snippet)
	}
}

/** Facets writes the Facets list: for each facet its label and a list of its
 * values, each a link that runs the search again restricted to it. The link is
 * a query: ?q= and the query encoded, and &set= and the set when the search was
 * of one set.
 *
 * Parameters:
 *   w (io.Writer)                  — where to write
 *   facets ([]corpus.FacetField)   — the result's facets
 *   set (string)                   — the set the search was of; empty for every set
 *
 * Example:
 *   render.Facets(os.Stdout, res.Facets, "")
 */
func Facets(w io.Writer, facets []corpus.FacetField, set string) {
	fmt.Fprintf(w, "- Facets\n")
	for _, f := range facets {
		fmt.Fprintf(w, "  - %s\n", Text(strings.TrimSpace(Clean(f.Label))))
		for _, v := range f.Values {
			target := "?q=" + url.QueryEscape(v.Query)
			if set != "" {
				target += "&set=" + url.QueryEscape(set)
			}
			fmt.Fprintf(w, "    - [%s (%d)](%s)\n", Text(strings.TrimSpace(Clean(v.Value))), v.Count, target)
		}
	}
}

/** Body writes the Markdown of a result: the Facets list and a Results list when
 * the result has facets, the hits alone otherwise.
 *
 * Parameters:
 *   w (io.Writer)       — where to write
 *   res (*corpus.Results) — the result
 *   set (string)        — the set the search was of; empty for every set
 *
 * Example:
 *   render.Body(os.Stdout, res, "")
 */
func Body(w io.Writer, res *corpus.Results, set string) {
	indent := ""
	if len(res.Facets) > 0 { // facets, when a set defines them and the result has values, come before the results
		Facets(w, res.Facets, set)
		fmt.Fprintf(w, "- Results\n")
		indent = "  "
	}
	for _, h := range res.Hits {
		detail := h.Tier
		if h.Pinned {
			detail += ", pinned"
		}
		if len(h.Tags) > 0 {
			detail += ", tags: " + Text(strings.Join(h.Tags, " "))
		}
		Hit(w, indent, h.Title, h.Address, h.Anchor, Text(detail), Snippet(h.Snippet))
	}
}

/** FacetsJSON is the facets of a result as an object by field: each has its
 * label and its values, each value its count and the query that follows it.
 *
 * Parameters:
 *   facets ([]corpus.FacetField) — the result's facets
 *
 * Returns:
 *   map[string]any — by field name; empty when there are none
 *
 * Example:
 *   m := render.FacetsJSON(res.Facets)
 */
func FacetsJSON(facets []corpus.FacetField) map[string]any {
	out := map[string]any{}
	for _, f := range facets {
		vals := make([]map[string]any, 0, len(f.Values))
		for _, v := range f.Values {
			vals = append(vals, map[string]any{"value": v.Value, "count": v.Count, "query": v.Query})
		}
		out[f.Field] = map[string]any{"label": f.Label, "values": vals}
	}
	return out
}

/** JSON is the object `search -json` prints for a result: hits, searched,
 * pending, facets, and filtered when a filter was asked for.
 *
 * Parameters:
 *   res (*corpus.Results) — the result
 *   filtered (bool)       — whether a tag or pinned option was given
 *
 * Returns:
 *   map[string]any — the object, ready to marshal
 *
 * Example:
 *   b, _ := json.Marshal(render.JSON(res, false))
 */
func JSON(res *corpus.Results, filtered bool) map[string]any {
	hits := make([]map[string]any, 0, len(res.Hits))
	for _, h := range res.Hits {
		ts := h.Tags
		if ts == nil {
			ts = []string{}
		}
		hits = append(hits, map[string]any{"address": h.Address, "title": h.Title, "site": h.Site, "tier": h.Tier, "snippet": h.Snippet, "anchor": h.Anchor, "pinned": h.Pinned, "tags": ts})
	}
	m := map[string]any{"hits": hits, "searched": res.Searched, "pending": res.Pending, "facets": FacetsJSON(res.Facets)}
	if filtered {
		m["filtered"] = res.Filtered
	}
	return m
}
