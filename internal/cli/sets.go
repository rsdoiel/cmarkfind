package cli

import (
	"context"
	"encoding/json"
	"errors"
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
		fields := "default"
		if s.Fields != "" {
			fields = "custom"
		}
		mp, merr := db.FieldMapping(ctx, s.Name)
		if merr != nil {
			return merr
		}
		if o.json {
			m := map[string]any{"name": s.Name, "source": s.Source, "pages": s.Pages, "ignored": s.Ignored,
				"fields": fields, "fields_version": s.FieldsVersion, "index": s.IndexDir, "built": s.Built, "facets": facetDefsJSON(mp)}
			if s.Root != "" {
				m["root"] = s.Root
			}
			b, _ := json.Marshal(m)
			fmt.Fprintf(o.stdout, "%s\n", b)
			return nil
		}
		fmt.Fprintf(o.stdout, "name %s\nsource %s\npages %d\nignored %t\nfields %s\nindex %s\nbuilt %t\n", s.Name, s.Source, s.Pages, s.Ignored, fields, s.IndexDir, s.Built)
		if s.Root != "" {
			fmt.Fprintf(o.stdout, "root %s\n", s.Root)
		}
		var fl []string
		for _, f := range mp.Facets() {
			fl = append(fl, fmt.Sprintf("%s (%s)", f.Label, f.Field))
		}
		if len(fl) == 0 {
			fl = []string{"none"}
		}
		fmt.Fprintf(o.stdout, "facets %s\n", strings.Join(fl, ", "))
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

// setFields shows how a set reads front matter, assigns roles to fields, or
// with -reset returns the set to the default mapping.
func (o *out) setFields(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("set fields", o)
	reset := fset.Bool("reset", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() == 0 {
		return fmt.Errorf("%w: set fields takes a SET", ErrUsage)
	}
	if *reset && fset.NArg() > 1 {
		return fmt.Errorf("%w: -reset takes only a SET", ErrUsage)
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
	switch {
	case *reset:
		err = db.SetFields(ctx, site, 0, "")
	case fset.NArg() > 1:
		err = db.AssignFields(ctx, site, fset.Args()[1:])
	}
	if errors.Is(err, corpus.ErrRemoteSet) {
		return fmt.Errorf("%w: %v; its field mapping is the publisher's", ErrUsage, err)
	}
	if err != nil {
		return err
	}
	if *reset || fset.NArg() > 1 { // a set builds its index when its mapping changes, as a fill does
		_, err := db.BuildSets(ctx, site, 0)
		o.indexWarnings(db)
		if err != nil {
			return err
		}
	}
	m, err := db.FieldMapping(ctx, site)
	if err != nil {
		return err
	}
	if o.json {
		fields := map[string][]string{}
		for _, f := range m.Fields() {
			fields[f] = m.Roles(f)
		}
		b, _ := json.Marshal(map[string]any{"name": site, "fields": fields, "facets": facetDefsJSON(m)})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	for _, f := range m.Fields() {
		line := fmt.Sprintf("%-14s %s", f, strings.Join(m.Roles(f), ","))
		if l := m.Label(f); l != "" {
			line += fmt.Sprintf(" %q", l)
		}
		fmt.Fprintln(o.stdout, line)
	}
	return nil
}

// facetDefsJSON lists a mapping's facets for JSON: the field, its label and
// whether it is a facet of years.
func facetDefsJSON(m *corpus.Mapping) []map[string]any {
	out := []map[string]any{}
	for _, f := range m.Facets() {
		out = append(out, map[string]any{"field": f.Field, "label": f.Label, "date": f.Date})
	}
	return out
}
