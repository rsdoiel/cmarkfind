package index

import (
	"os"
	"path/filepath"
	"testing"
)

// Wider copies of the on-disk records: each carries one unknown trailing
// element, as a later writer of format version 1 may add (format.cddl, VERSION RULE).
type (
	anchorX struct {
		_               struct{} `cbor:",toarray"`
		Slug, Heading   string
		Position, Level uint64
		Extra           string
	}
	fragmentX struct {
		Address   string         `cbor:"0,keyasint"`
		Text      string         `cbor:"1,keyasint"`
		WordCount uint64         `cbor:"2,keyasint"`
		Fields    map[string]any `cbor:"3,keyasint,omitempty"`
		Anchors   []anchorX      `cbor:"4,keyasint,omitempty"`
		Segments  [][2]uint64    `cbor:"5,keyasint,omitempty"`
	}
	entryX struct {
		_        struct{} `cbor:",toarray"`
		Word     string
		Postings []posting
		Extra    string
	}
	chunkX struct {
		_          struct{} `cbor:",toarray"`
		Version    int
		Analyzer   string
		Generation uint64
		Entries    []entryX
		Extra      string
	}
	rowX struct {
		_         struct{} `cbor:",toarray"`
		WordCount uint64
		Fragment  []byte
		Address   string
		Extra     string
	}
	tableX struct {
		_          struct{} `cbor:",toarray"`
		Version    int
		Analyzer   string
		Generation uint64
		Rows       []rowX
		Extra      string
	}
)

func trailingFixture() []Doc {
	return []Doc{
		{Address: "https://example.org/a.md", Title: "Soil", Body: "# Soil\n\nTomatoes like warm soil.\n\n## Seeds\n\nSeeds go in early."},
		{Address: "https://example.org/b.md", Title: "Other", Body: "Nothing here."},
	}
}

func TestAnUnknownTrailingElementOfARecordIsIgnored(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"anchor": func(t *testing.T, dir string) {
			ix := open(t, dir)
			fr, err := ix.fragment(0)
			if err != nil {
				t.Fatal(err)
			}
			x := fragmentX{Address: fr.Address, Text: fr.Text, WordCount: fr.WordCount, Fields: fr.Fields, Segments: fr.Segments}
			for _, a := range fr.Anchors {
				x.Anchors = append(x.Anchors, anchorX{Slug: a.Slug, Heading: a.Heading, Position: a.Position, Level: a.Level, Extra: "later"})
			}
			if len(x.Anchors) == 0 {
				t.Fatal("fixture has no anchors")
			}
			raw, err := encMode.Marshal(x)
			if err != nil {
				t.Fatal(err)
			}
			swapFragment(t, dir, 0, raw)
		},
		"chunk and entry": func(t *testing.T, dir string) {
			m := manifestOf(t, dir)
			for i, c := range m.Chunks {
				raw, err := readFile(os.DirFS(dir), "w", m.Compression, c.File)
				if err != nil {
					t.Fatal(err)
				}
				var wc wordChunk
				if err := decMode.Unmarshal(raw, &wc); err != nil {
					t.Fatal(err)
				}
				x := chunkX{Version: wc.Version, Analyzer: wc.Analyzer, Generation: wc.Generation, Extra: "later"}
				for _, e := range wc.Entries {
					x.Entries = append(x.Entries, entryX{Word: e.Word, Postings: e.Postings, Extra: "later"})
				}
				ref, err := encodeFile(dir, "w", m.Compression, x)
				if err != nil {
					t.Fatal(err)
				}
				m.Chunks[i].File = ref
			}
			writeManifest(t, dir, m)
		},
		"document table and row": func(t *testing.T, dir string) {
			m := manifestOf(t, dir)
			raw, err := readFile(os.DirFS(dir), "d", m.Compression, m.Documents.Ranges[0].File)
			if err != nil {
				t.Fatal(err)
			}
			var dt documentTable
			if err := decMode.Unmarshal(raw, &dt); err != nil {
				t.Fatal(err)
			}
			x := tableX{Version: dt.Version, Analyzer: dt.Analyzer, Generation: dt.Generation, Extra: "later"}
			for _, r := range dt.Rows {
				x.Rows = append(x.Rows, rowX{WordCount: r.WordCount, Fragment: r.Fragment, Address: r.Address, Extra: "later"})
			}
			ref, err := encodeFile(dir, "d", m.Compression, x)
			if err != nil {
				t.Fatal(err)
			}
			m.Documents.Ranges[0].File = ref
			writeManifest(t, dir, m)
		},
	}
	for name, widen := range cases {
		t.Run(name, func(t *testing.T) {
			dir := build(t, trailingFixture(), 4)
			widen(t, dir)
			_ = filepath.Join
			ix := open(t, dir)
			hits, err := search(ix, "tomatoes", 10)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(hits) != 1 || hits[0].Address != "https://example.org/a.md" {
				t.Fatalf("hits %v", addrs(hits))
			}
			if _, err := ix.Excerpt(mustParse(t, "seeds"), hits[0].Doc, 10); err != nil {
				t.Fatalf("excerpt: %v", err)
			}
		})
	}
}

func mustParse(t *testing.T, q string) *Node {
	t.Helper()
	n, err := ParseQuery(q, nil)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
