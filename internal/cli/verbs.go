package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// flagJSON is a flag as `verbs -json` writes it.
type flagJSON struct {
	Name string `json:"name"`
	Bool bool   `json:"bool"`
}

func flagsJSON(path string) ([]flagJSON, error) {
	fl, err := verbFlags(path)
	if err != nil {
		return nil, err
	}
	sort.Slice(fl, func(i, j int) bool { return fl[i].Name < fl[j].Name })
	out := make([]flagJSON, len(fl))
	for i, f := range fl {
		out[i] = flagJSON{f.Name, f.Bool}
	}
	return out, nil
}

/** verbs is `cmarkfind verbs [-json]`: the registry, read out. It opens no corpus.
 *
 * Parameters:
 *   ctx (context.Context) — unused
 *   dbFile (string)       — unused
 *   args ([]string)       — the options after the verb
 *
 * Returns:
 *   error — a usage error for an argument or an unknown option
 *
 * Example:
 *   code := cli.Run([]string{"verbs", "-json"}, os.Stdout, os.Stderr)
 */
func (o *out) verbs(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("verbs", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 0 {
		return fmt.Errorf("%w: verbs takes no arguments", ErrUsage)
	}
	specs := verbSpecs()
	if !o.json {
		width := 0
		for _, v := range specs {
			width = max(width, len(v.Name))
		}
		for _, v := range specs {
			fmt.Fprintf(o.stdout, "%-*s  %s\n", width, v.Name, v.Summary)
		}
		return nil
	}
	type item struct {
		Name     string                `json:"name"`
		Summary  string                `json:"summary"`
		Subverbs []string              `json:"subverbs"`
		Flags    []flagJSON            `json:"flags"`
		Sub      map[string][]flagJSON `json:"sub,omitempty"`
	}
	var list []item
	for _, v := range specs {
		it := item{Name: v.Name, Summary: v.Summary, Subverbs: append([]string{}, v.Subverbs...)}
		if len(v.Subverbs) == 0 {
			fl, err := flagsJSON(v.Name)
			if err != nil {
				return err
			}
			it.Flags = fl
		} else {
			it.Flags = []flagJSON{}
			it.Sub = map[string][]flagJSON{}
			for _, s := range v.Subverbs {
				fl, err := flagsJSON(v.Name + " " + s)
				if err != nil {
					return err
				}
				it.Sub[s] = fl
			}
		}
		list = append(list, it)
	}
	b, err := json.Marshal(map[string]any{"verbs": list})
	if err != nil {
		return err
	}
	fmt.Fprintf(o.stdout, "%s\n", b)
	return nil
}
