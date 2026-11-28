package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rsdoiel/cmarkfind/corpus"
)

func (o *out) pin(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("pin", o)
	list := fset.Bool("list", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if (*list && fset.NArg() != 0) || (!*list && fset.NArg() == 0) {
		return fmt.Errorf("%w: pin takes one or more ADDRESS, or -list", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	if *list {
		pins, err := db.Pins(ctx)
		if err != nil {
			return err
		}
		if o.json {
			out := make([]map[string]string, 0, len(pins))
			for _, p := range pins {
				out = append(out, map[string]string{"address": p.Address, "title": p.Title, "state": string(p.State), "added_at": p.AddedAt})
			}
			b, _ := json.Marshal(map[string]any{"pinned": out})
			fmt.Fprintf(o.stdout, "%s\n", b)
			return nil
		}
		for _, p := range pins {
			fmt.Fprintf(o.stdout, "%s  %s  [%s]\n", p.Address, clean(p.Title), p.State)
		}
		return nil
	}
	n, err := db.Pin(ctx, fset.Args()...)
	if err != nil {
		return err
	}
	pins, err := db.Pins(ctx)
	if err != nil {
		return err
	}
	given := map[string]bool{}
	for _, a := range fset.Args() {
		given[a] = true
	}
	for _, p := range pins {
		if given[p.Address] && p.State == corpus.PinNotInCorpus {
			o.note("%s is not in the corpus; the pin applies when it is filled", p.Address)
		}
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"pinned": n})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "pinned %d %s\n", n, plural(n, "page", "pages"))
	}
	return nil
}

func (o *out) unpin(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("unpin", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() == 0 {
		return fmt.Errorf("%w: unpin takes one or more ADDRESS", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := db.Unpin(ctx, fset.Args()...)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"unpinned": n})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "unpinned %d %s\n", n, plural(n, "page", "pages"))
	}
	if n == 0 {
		o.note("nothing was pinned")
		return ErrNoMatch
	}
	return nil
}

func (o *out) tag(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("tag", o)
	list := fset.Bool("list", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if (*list && fset.NArg() > 1) || (!*list && fset.NArg() < 2) {
		return fmt.Errorf("%w: tag takes ADDRESS TAG..., or -list [ADDRESS]", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	if *list {
		if fset.NArg() == 1 {
			ts, err := db.TagsOf(ctx, fset.Arg(0))
			if err != nil {
				return err
			}
			if ts == nil {
				ts = []string{}
			}
			if o.json {
				b, _ := json.Marshal(map[string]any{"address": fset.Arg(0), "tags": ts})
				fmt.Fprintf(o.stdout, "%s\n", b)
				return nil
			}
			for _, t := range ts {
				fmt.Fprintln(o.stdout, t)
			}
			return nil
		}
		cs, err := db.Tags(ctx)
		if err != nil {
			return err
		}
		if o.json {
			out := make([]map[string]any, 0, len(cs))
			for _, c := range cs {
				out = append(out, map[string]any{"tag": c.Tag, "pages": c.Pages})
			}
			b, _ := json.Marshal(map[string]any{"tags": out})
			fmt.Fprintf(o.stdout, "%s\n", b)
			return nil
		}
		for _, c := range cs {
			fmt.Fprintf(o.stdout, "%s  %d\n", c.Tag, c.Pages)
		}
		return nil
	}
	n, err := db.Tag(ctx, fset.Arg(0), fset.Args()[1:]...)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"address": fset.Arg(0), "tagged": n})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "added %d %s\n", n, plural(n, "tag", "tags"))
	}
	return nil
}

func (o *out) untag(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("untag", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() < 2 {
		return fmt.Errorf("%w: untag takes ADDRESS TAG...", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := db.Untag(ctx, fset.Arg(0), fset.Args()[1:]...)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"address": fset.Arg(0), "removed": n})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "removed %d %s\n", n, plural(n, "tag", "tags"))
	}
	if n == 0 {
		o.note("the page did not carry that tag")
		return ErrNoMatch
	}
	return nil
}
