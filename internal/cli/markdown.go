package cli

import (
	"fmt"
	"strings"
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
	fmt.Fprintf(o.stdout, "- [%s](%s)\n", mdText(text), dest)
	if detail != "" {
		fmt.Fprintf(o.stdout, "  %s\n", detail)
	}
	if snippet != "" {
		fmt.Fprintf(o.stdout, "  %s\n", snippet)
	}
}
