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

func (o *out) indexer(ctx context.Context, dbFile string, args []string) error {
	verb := "list" // a bare indexer lists
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	switch verb {
	case "build":
		return o.indexBuild(ctx, dbFile, args)
	case "validate":
		return o.indexValidate(ctx, dbFile, args)
	case "list":
		return o.indexList(ctx, dbFile, args)
	case "discover":
		return o.indexDiscover(ctx, dbFile, args)
	case "add":
		return o.indexAdd(ctx, dbFile, args)
	}
	return fmt.Errorf("%w: unknown indexer verb %q (build, validate, list, discover, add)", ErrUsage, verb)
}

// indexSets resolves the optional SET argument of an indexer verb: the one set,
// or every set.
func indexSets(ctx context.Context, db *corpus.DB, verb string, args []string) ([]corpus.SetInfo, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%w: indexer %s takes at most one SET", ErrUsage, verb)
	}
	sets, err := db.Sets(ctx)
	if err != nil || len(args) == 0 {
		return sets, err
	}
	site, err := db.ResolveSet(ctx, args[0])
	if err != nil {
		return nil, err
	}
	for _, s := range sets {
		if s.Name == site {
			return []corpus.SetInfo{s}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", corpus.ErrNoSet, args[0])
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
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sets, err := indexSets(ctx, db, "build", fset.Args())
	if err != nil {
		return err
	}
	site := ""
	if fset.NArg() == 1 && len(sets) == 1 {
		site = sets[0].Name
	}
	b, err := db.BuildSets(ctx, site, *chunk)
	o.indexWarnings(db)
	if err != nil {
		return err
	}
	if o.json {
		out, _ := json.Marshal(map[string]any{"built": b.Built, "unchanged": b.Unchanged})
		fmt.Fprintf(o.stdout, "%s\n", out)
		return nil
	}
	fmt.Fprintf(o.stdout, "built %d, unchanged %d\n", b.Built, b.Unchanged)
	return nil
}

func (o *out) indexValidate(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("indexer validate", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sets, err := indexSets(ctx, db, "validate", fset.Args())
	if err != nil {
		return err
	}
	var first error
	rows := []map[string]any{}
	for _, s := range sets {
		verr := index.Verify(s.IndexDir)
		if verr != nil && first == nil {
			first = fmt.Errorf("%s: %w", s.Name, verr)
		}
		rows = append(rows, map[string]any{"name": s.Name, "path": s.IndexDir, "ok": verr == nil})
		if !o.json && verr == nil {
			fmt.Fprintf(o.stdout, "%s is sound\n", s.Name)
		}
	}
	if o.json {
		out, _ := json.Marshal(map[string]any{"indexes": rows})
		fmt.Fprintf(o.stdout, "%s\n", out)
	}
	return first
}

func (o *out) indexList(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("indexer list", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sets, err := indexSets(ctx, db, "list", fset.Args())
	if err != nil {
		return err
	}
	rows := []map[string]any{}
	for _, s := range sets {
		ix, ierr := index.Open(s.IndexDir)
		if ierr != nil && !index.IsMissing(ierr) {
			return fmt.Errorf("%s: %w", s.Name, ierr)
		}
		if ierr != nil {
			rows = append(rows, map[string]any{"name": s.Name, "path": s.IndexDir, "built": false})
			if !o.json {
				fmt.Fprintf(o.stdout, "%s\nnot built\n", s.Name)
			}
			continue
		}
		docOnly := ix.DocumentOnly()
		if docOnly == nil {
			docOnly = []string{}
		}
		rows = append(rows, map[string]any{"name": s.Name, "path": s.IndexDir, "built": true, "pages": ix.Count(), "chunks": ix.Chunks(), "generation": ix.Generation(), "built_at": ix.Built(), "document_only": docOnly})
		if !o.json {
			fmt.Fprintf(o.stdout, "%s\npath %s\npages %d\nchunks %d\ngeneration %d\nbuilt %s\n", s.Name, s.IndexDir, ix.Count(), ix.Chunks(), ix.Generation(), ix.Built())
			if len(docOnly) > 0 {
				fmt.Fprintf(o.stdout, "document-only %d  %s\n", len(docOnly), strings.Join(docOnly, " "))
			}
		}
	}
	if o.json {
		out, _ := json.Marshal(map[string]any{"indexes": rows})
		fmt.Fprintf(o.stdout, "%s\n", out)
	}
	return nil
}
