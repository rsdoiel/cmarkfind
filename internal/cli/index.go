package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/index"
)

/** DefaultIndexDir returns where the index lives when no directory is named:
 * the CMARKFIND_INDEX variable, else an index folder beside the corpus file.
 *
 * Parameters:
 *   dbFile (string) — the corpus file in use
 *
 * Returns:
 *   string — the directory
 *
 * Example:
 *   d := cli.DefaultIndexDir("/home/me/.local/share/cmarkfind/corpus.db") // ".../cmarkfind/index"
 */
func DefaultIndexDir(dbFile string) string { return corpus.DefaultIndexDir(dbFile) }

// indexDir takes the optional DIR argument of an index verb.
func indexDir(dbFile, verb string, args []string) (string, error) {
	switch len(args) {
	case 0:
		return DefaultIndexDir(dbFile), nil
	case 1:
		return args[0], nil
	}
	return "", fmt.Errorf("%w: indexer %s takes at most one DIR", ErrUsage, verb)
}

// openIndex opens an index, naming the directory when it is not there.
func openIndex(dir string) (*index.Index, error) {
	ix, err := index.Open(dir)
	if err != nil {
		return nil, err
	}
	return ix, nil
}

func (o *out) indexer(ctx context.Context, dbFile string, args []string) error {
	verb := "list" // a bare indexer lists
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	switch verb {
	case "build":
		return o.indexBuild(ctx, dbFile, args)
	case "validate":
		return o.indexValidate(dbFile, args)
	case "list":
		return o.indexList(dbFile, args)
	}
	return fmt.Errorf("%w: unknown indexer verb %q (build, validate, list)", ErrUsage, verb)
}

func (o *out) indexBuild(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("indexer build", o)
	chunk := fset.Int("chunk", 0, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	chunkSet := false
	fset.Visit(func(f *flag.Flag) { chunkSet = chunkSet || f.Name == "chunk" })
	if chunkSet && *chunk < 1 {
		return fmt.Errorf("%w: -chunk must be at least 1", ErrUsage)
	}
	dir, err := indexDir(dbFile, "build", fset.Args())
	if err != nil {
		return err
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	ix, err := db.BuildIndex(ctx, dir, *chunk)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"path": dir, "pages": ix.Count(), "chunks": ix.Chunks(), "generation": ix.Generation()})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "indexed %d %s in %d %s, generation %d, into %s\n", ix.Count(), plural(ix.Count(), "page", "pages"),
		ix.Chunks(), plural(ix.Chunks(), "chunk", "chunks"), ix.Generation(), dir)
	return nil
}

func (o *out) indexValidate(dbFile string, args []string) error {
	fset, j := flags("indexer validate", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	dir, err := indexDir(dbFile, "validate", fset.Args())
	if err != nil {
		return err
	}
	if err := index.Verify(dir); err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"path": dir, "ok": true})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "%s is sound\n", dir)
	return nil
}

func (o *out) indexList(dbFile string, args []string) error {
	fset, j := flags("indexer list", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	dir, err := indexDir(dbFile, "list", fset.Args())
	if err != nil {
		return err
	}
	ix, err := openIndex(dir)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"path": dir, "pages": ix.Count(), "chunks": ix.Chunks(), "generation": ix.Generation(), "built": ix.Built()})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "path %s\npages %d\nchunks %d\ngeneration %d\nbuilt %s\n", dir, ix.Count(), ix.Chunks(), ix.Generation(), ix.Built())
	return nil
}

// searchIndex answers a search from an index directory.
func (o *out) searchIndex(dir string, query string, limit int) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("%w: search needs a query", ErrUsage)
	}
	node, err := index.ParseQuery(query, nil)
	if err != nil {
		return err
	}
	ix, err := openIndex(dir)
	if err != nil {
		return err
	}
	var hits []index.Hit
	if node != nil {
		if hits, err = ix.Search(node, limit); err != nil {
			return err
		}
	}
	type row struct {
		address, title string
		score          float64
		ex             index.Excerpt
	}
	rows := make([]row, 0, len(hits))
	for _, h := range hits {
		f, err := ix.Fragment(h.Doc)
		if err != nil {
			return err
		}
		ex, err := ix.Excerpt(node, h.Doc, 0)
		if err != nil {
			return err
		}
		rows = append(rows, row{h.Address, f.Title, h.Score, ex})
	}
	if o.json {
		hs := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			h := map[string]any{"address": r.address, "title": r.title, "score": r.score, "excerpt": r.ex.HTML}
			if r.ex.Anchor != "" {
				h["anchor"] = r.ex.Anchor
			}
			hs = append(hs, h)
		}
		b, _ := json.Marshal(map[string]any{"hits": hs, "searched": ix.Count()})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		for _, r := range rows {
			text := ""
			if r.ex.Text != "" {
				lead, trail := "", ""
				if r.ex.Leading {
					lead = "… "
				}
				if r.ex.Trailing {
					trail = " …"
				}
				text = mdSnippet(lead + r.ex.Text + trail)
			}
			o.writeHit(r.title, r.address, r.ex.Anchor, "", text)
		}
		fmt.Fprintf(o.stderr, "%s: %d %s, searched %d pages\n", appName, len(rows), plural(len(rows), "hit", "hits"), ix.Count())
	}
	if len(rows) == 0 {
		return ErrNoMatch
	}
	return nil
}
