package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rsdoiel/cmarkfind/corpus"
)

// mdText escapes what would turn page text into Markdown markup, and drops
// control characters.
func mdText(s string) string {
	var b strings.Builder
	for _, r := range clean(s) {
		switch r {
		case '\\', '[', ']', '*', '_', '`', '<', '>':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mdDest makes an address safe as the destination of an inline link: the
// characters that would end or break it are percent-encoded.
func mdDest(s string) string {
	var b strings.Builder
	for _, c := range []byte(clean(s)) {
		switch {
		case c <= ' ', c == '(', c == ')', c == '<', c == '>', c == '\\', c == 0x7f:
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// mdSnippet turns a snippet's [matched] words into **bold** and escapes the
// rest.
func mdSnippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range clean(s) {
		switch r {
		case '[':
			b.WriteString("**")
		case ']':
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

// writeHit prints one hit as a Markdown list item: a link, then an optional
// detail line and an optional snippet, indented under it.
func (o *out) writeHit(title, address, anchor, detail, snippet string) {
	text := strings.TrimSpace(clean(title))
	if text == "" {
		text = address
	}
	dest := mdDest(address)
	if anchor != "" {
		dest += "#" + mdDest(anchor)
	}
	fmt.Fprintf(o.stdout, "%s- [%s](%s)\n", o.hitIndent, mdText(text), dest)
	if detail != "" {
		fmt.Fprintf(o.stdout, "%s  %s\n", o.hitIndent, detail)
	}
	if snippet != "" {
		fmt.Fprintf(o.stdout, "%s  %s\n", o.hitIndent, snippet)
	}
}

// writeFacets prints the Facets list: for each facet its label and a list of its
// values, each a link that runs the search again restricted to it. The link is a
// query: ?q= and the query encoded, and &set= and the set when the search was of
// one set.
func (o *out) writeFacets(facets []corpus.FacetField, set string) {
	fmt.Fprintf(o.stdout, "- Facets\n")
	for _, f := range facets {
		fmt.Fprintf(o.stdout, "  - %s\n", mdText(strings.TrimSpace(clean(f.Label))))
		for _, v := range f.Values {
			target := "?q=" + url.QueryEscape(v.Query)
			if set != "" {
				target += "&set=" + url.QueryEscape(set)
			}
			fmt.Fprintf(o.stdout, "    - [%s (%d)](%s)\n", mdText(strings.TrimSpace(clean(v.Value))), v.Count, target)
		}
	}
}

// facetsJSON is the facets of a result as an object by field: each has its label
// and its values, each value its count and the query that follows it.
func facetsJSON(facets []corpus.FacetField) map[string]any {
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
