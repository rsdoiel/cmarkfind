package cli

import "github.com/rsdoiel/cmarkfind/internal/render"

// The Markdown of a result is written by package render, which the server uses
// too, so that the two print the same bytes. These are the names the verbs use.

func mdText(s string) string    { return render.Text(s) }
func mdSnippet(s string) string { return render.Snippet(s) }
func clean(s string) string     { return render.Clean(s) }
