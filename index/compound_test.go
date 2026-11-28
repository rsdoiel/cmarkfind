package index

import "testing"

// A hyphen or underscore ends a word, so a compound written with one is
// indexed as its parts; a query written with one is the phrase of its parts,
// and a leading minus is still NOT.
func TestACompoundQueryIsAPhraseOfItsParts(t *testing.T) {
	docs := []Doc{
		{Address: "https://example.org/1.md", Title: "One", Body: "A state-of-the-art engine and a snake_case name."},
		{Address: "https://example.org/2.md", Title: "Two", Body: "The art of the state. Case for a snake."},
	}
	ix := open(t, build(t, docs, 0))
	cases := []struct {
		query string
		want  []string
	}{
		{"state-of-the-art", []string{"https://example.org/1.md"}},
		{"snake_case", []string{"https://example.org/1.md"}},
		{"snake case", []string{"https://example.org/1.md", "https://example.org/2.md"}},
		{"-snake", nil},
		{"snake -case", nil},
		{"state -art", nil},
		{"engine -snake_case", nil},
		{"engine -snake", nil},
	}
	for _, c := range cases {
		hits, err := search(ix, c.query, 10)
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		got := map[string]bool{}
		for _, a := range addrs(hits) {
			got[a] = true
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.query, addrs(hits), c.want)
			continue
		}
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%q: got %v, want %v", c.query, addrs(hits), c.want)
			}
		}
	}
}
