package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

/** flagInfo describes one flag of a verb.
 *
 * Fields:
 *   Name (string) — the flag's name, without dashes
 *   Bool (bool)   — true for a switch, false when it takes a value
 *
 * Example:
 *   fl, _ := verbFlags("fill") // fl includes flagInfo{"site-url", false}
 */
type flagInfo struct {
	Name string
	Bool bool
}

/** verbSpec is one entry of the verb registry: the single place that says which
 * verbs there are. Run's dispatch, the error messages, `verbs` and shell
 * completion all read it, and a terminal interface can.
 *
 * Fields:
 *   Name (string)       — the verb as typed
 *   Summary (string)    — one line, the verb's help topic summary
 *   Subverbs ([]string) — the words that may follow the verb (indexer)
 *   run                 — the verb itself
 *   static ([]flagInfo) — the flags of a verb that does not use a FlagSet (help)
 *
 * Example:
 *   for _, v := range verbSpecs() { fmt.Println(v.Name, v.Summary) }
 */
type verbSpec struct {
	Name, Summary string
	Subverbs      []string
	run           func(o *out, ctx context.Context, dbFile string, args []string) error
	static        []flagInfo
}

// Paths are the names verbFlags understands for this verb: the verb, or each of
// its subverbs as "verb sub".
func (v verbSpec) Paths() []string {
	if len(v.Subverbs) == 0 {
		return []string{v.Name}
	}
	var p []string
	for _, s := range v.Subverbs {
		p = append(p, v.Name+" "+s)
	}
	return p
}

/** verbSpecs returns the registry, in the order the help page lists the verbs.
 * It is a function so that the verbs it names may call it.
 *
 * Returns:
 *   []verbSpec — every verb, help last
 *
 * Example:
 *   names := verbNames() // fill, search, indexer, ...
 */
func verbSpecs() []verbSpec {
	specs := []verbSpec{
		{Name: "fill", run: (*out).fill},
		{Name: "search", run: (*out).search},
		{Name: "search-set", run: (*out).searchSet},
		{Name: "set", Subverbs: []string{"list", "show", "ignore", "unignore", "fields"}, run: (*out).set},
		{Name: "indexer", Subverbs: []string{"build", "validate", "list", "discover"}, run: (*out).indexer},
		{Name: "stats", run: (*out).stats},
		{Name: "forget", run: (*out).forget},
		{Name: "exclude", run: (*out).exclude},
		{Name: "unexclude", run: (*out).unexclude},
		{Name: "pin", run: (*out).pin},
		{Name: "unpin", run: (*out).unpin},
		{Name: "tag", run: (*out).tag},
		{Name: "untag", run: (*out).untag},
		{Name: "verbs", run: (*out).verbs},
		{Name: "completion", run: (*out).completion},
		{Name: "help", Summary: "show this page or one topic's page", static: []flagInfo{{"json", true}}},
	}
	for i := range specs {
		if specs[i].Summary == "" {
			specs[i].Summary = topicSummary(specs[i].Name)
		}
	}
	return specs
}

func topicSummary(name string) string {
	for _, t := range Topics {
		if t.Name == name {
			return t.Summary
		}
	}
	return ""
}

func verbNames() []string {
	var n []string
	for _, v := range verbSpecs() {
		n = append(n, v.Name)
	}
	return n
}

func verbList() string { return strings.Join(verbNames(), ", ") }

func findVerb(name string) (verbSpec, bool) {
	for _, v := range verbSpecs() {
		if v.Name == name {
			return v, true
		}
	}
	return verbSpec{}, false
}

// errProbed is what parse returns when it was only asked for the FlagSet.
var errProbed = errors.New("flags read")

/** verbFlags returns the flags of a verb, read from the verb's own FlagSet: the
 * verb runs until it first parses its options and stops there, before it opens
 * anything. A verb that does something first is an error here (a test reads
 * every verb this way).
 *
 * Parameters:
 *   path (string) — a verb, or "indexer build" for a verb's subverb
 *
 * Returns:
 *   []flagInfo — the flags in name order, -json included
 *   error      — the verb is unknown or did not parse its options first
 *
 * Example:
 *   fl, err := verbFlags("search") // -index, -json, -limit, -pinned, -tag
 */
func verbFlags(path string) ([]flagInfo, error) {
	name, sub, _ := strings.Cut(path, " ")
	v, ok := findVerb(name)
	if !ok {
		return nil, fmt.Errorf("no verb %q", name)
	}
	if v.static != nil {
		return v.static, nil
	}
	o := &out{stdout: io.Discard, stderr: io.Discard, probing: true}
	var args []string
	if sub != "" {
		args = []string{sub}
	}
	if err := v.run(o, context.Background(), "", args); !errors.Is(err, errProbed) || o.probed == nil {
		return nil, fmt.Errorf("verb %q did not parse its options first (%v)", path, err)
	}
	var fl []flagInfo
	o.probed.VisitAll(func(f *flag.Flag) {
		b, isBool := f.Value.(interface{ IsBoolFlag() bool })
		fl = append(fl, flagInfo{f.Name, isBool && b.IsBoolFlag()})
	})
	return fl, nil
}
