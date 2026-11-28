package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsdoiel/cmarkfind/corpus"
)

// set is the verb for index sets: list, show, ignore, unignore, fields. A set is
// one site instance, made by fill and removed by forget.
func (o *out) set(ctx context.Context, dbFile string, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%w: set takes list, show, ignore, unignore or fields", ErrUsage)
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "list":
		return o.setList(ctx, dbFile, args)
	case "show":
		return o.setShow(ctx, dbFile, args)
	case "ignore":
		return o.setIgnore(ctx, dbFile, args, true)
	case "unignore":
		return o.setIgnore(ctx, dbFile, args, false)
	case "fields":
		return o.setFields(ctx, dbFile, args)
	}
	return fmt.Errorf("%w: unknown set verb %q (list, show, ignore, unignore, fields)", ErrUsage, sub)
}

func (o *out) setList(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("set list", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 0 {
		return fmt.Errorf("%w: set list takes no arguments", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sets, err := db.Sets(ctx)
	if err != nil {
		return err
	}
	if o.json {
		rows := make([]map[string]any, 0, len(sets))
		for _, s := range sets {
			rows = append(rows, map[string]any{"name": s.Name, "source": s.Source, "pages": s.Pages, "ignored": s.Ignored, "built": s.Built})
		}
		b, _ := json.Marshal(map[string]any{"sets": rows})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	for _, s := range sets {
		state := ""
		if s.Ignored {
			state = ", ignored"
		}
		fmt.Fprintf(o.stdout, "%s  %s, %d %s%s\n", s.Name, s.Source, s.Pages, plural(s.Pages, "page", "pages"), state)
	}
	return nil
}

// oneSet resolves the single SET argument of a set verb.
func (o *out) oneSet(ctx context.Context, db *corpus.DB, verb string, rest []string) (string, error) {
	if len(rest) != 1 {
		return "", fmt.Errorf("%w: %s takes one SET", ErrUsage, verb)
	}
	return db.ResolveSet(ctx, rest[0])
}

func (o *out) setShow(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("set show", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		return fmt.Errorf("%w: set show takes one SET", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	site, err := db.ResolveSet(ctx, fset.Arg(0))
	if err != nil {
		return err
	}
	sets, err := db.Sets(ctx)
	if err != nil {
		return err
	}
	for _, s := range sets {
		if s.Name != site {
			continue
		}
		fields := s.Fields
		if fields == "" {
			fields = "default"
		}
		if o.json {
			b, _ := json.Marshal(map[string]any{"name": s.Name, "source": s.Source, "pages": s.Pages, "ignored": s.Ignored,
				"fields": fields, "fields_version": s.FieldsVersion, "index": s.IndexDir, "built": s.Built})
			fmt.Fprintf(o.stdout, "%s\n", b)
			return nil
		}
		fmt.Fprintf(o.stdout, "name %s\nsource %s\npages %d\nignored %t\nfields %s\nindex %s\nbuilt %t\n", s.Name, s.Source, s.Pages, s.Ignored, fields, s.IndexDir, s.Built)
		return nil
	}
	return fmt.Errorf("%w: %s", corpus.ErrNoSet, fset.Arg(0))
}

func (o *out) setIgnore(ctx context.Context, dbFile string, args []string, ignore bool) error {
	verb := "set ignore"
	if !ignore {
		verb = "set unignore"
	}
	fset, j := flags(verb, o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	site, err := o.oneSet(ctx, db, verb, fset.Args())
	if err != nil {
		return err
	}
	if err := db.IgnoreSet(ctx, site, ignore); err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"name": site, "ignored": ignore})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else if ignore {
		fmt.Fprintf(o.stdout, "ignoring %s in search\n", site)
	} else {
		fmt.Fprintf(o.stdout, "searching %s again\n", site)
	}
	return nil
}

// setFields shows a set's field mapping, or with -reset clears it. Assigning
// roles to fields waits for the roles to be defined.
func (o *out) setFields(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("set fields", o)
	reset := fset.Bool("reset", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() > 1 {
		return fmt.Errorf("%w: set fields takes one SET; FIELD=ROLE assignments are not available yet", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	site, err := o.oneSet(ctx, db, "set fields", fset.Args())
	if err != nil {
		return err
	}
	if *reset {
		if err := db.SetFields(ctx, site, 0, ""); err != nil {
			return err
		}
	}
	sets, err := db.Sets(ctx)
	if err != nil {
		return err
	}
	for _, s := range sets {
		if s.Name != site {
			continue
		}
		fields := s.Fields
		if fields == "" {
			fields = "default"
		}
		if o.json {
			b, _ := json.Marshal(map[string]any{"name": s.Name, "fields": fields, "fields_version": s.FieldsVersion})
			fmt.Fprintf(o.stdout, "%s\n", b)
		} else {
			fmt.Fprintf(o.stdout, "%s\n", fields)
		}
	}
	return nil
}
