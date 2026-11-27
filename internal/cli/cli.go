package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsdoiel/cmarkfind"
	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/toc"
)

const appName = "cmarkfind"

/** DefaultDB returns where the corpus lives when neither -db nor CMARKFIND_DB
 * names it: cmarkfind/corpus.db in the user's data directory
 * ($XDG_DATA_HOME or ~/.local/share on Linux, ~/Library/Application Support on
 * macOS, %LocalAppData% on Windows).
 *
 * Returns:
 *   string — the path
 *
 * Example:
 *   p := cli.DefaultDB() // "/home/me/.local/share/cmarkfind/corpus.db"
 */
func DefaultDB() string { return corpus.DefaultPath() }

// out carries where a verb writes and whether it is in JSON mode.
type out struct {
	stdout, stderr io.Writer
	json           bool
}

/** Run runs the command line and returns the exit code (cmarkfind DR-0002).
 *
 * Parameters:
 *   args ([]string)    — the command line without the program name
 *   stdout (io.Writer) — results
 *   stderr (io.Writer) — errors, warnings and the search count
 *
 * Returns:
 *   int — the exit code
 *
 * Example:
 *   os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
 */
func Run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return RunContext(ctx, args, stdout, stderr)
}

/** RunContext is Run with a context that ends a long-running verb (fill
 * -every); Run passes one cancelled by Ctrl-C.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the verb
 *   args ([]string)       — the command line without the program name
 *   stdout (io.Writer)    — results
 *   stderr (io.Writer)    — errors, warnings and the search count
 *
 * Returns:
 *   int — the exit code
 *
 * Example:
 *   code := cli.RunContext(ctx, []string{"fill", "-batch", "50", "-every", "10m", addr}, os.Stdout, os.Stderr)
 */
func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o := &out{stdout: stdout, stderr: stderr}
	dbFile := os.Getenv("CMARKFIND_DB")
	help := func() {
		fmt.Fprint(stdout, cmarkfind.FmtHelp(HelpText, appName, cmarkfind.Version, cmarkfind.ReleaseDate, cmarkfind.ReleaseHash))
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		name, val, hasVal := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		switch name {
		case "help", "h":
			help()
			return ExitOK
		case "version":
			fmt.Fprintf(stdout, "%s %s %s\n", appName, cmarkfind.Version, cmarkfind.ReleaseHash)
			return ExitOK
		case "license":
			fmt.Fprint(stdout, strings.TrimLeft(cmarkfind.LicenseText, "\n"))
			return ExitOK
		case "db":
			args = args[1:]
			if !hasVal {
				if len(args) == 0 {
					return o.fail(fmt.Errorf("%w: -db needs a file", ErrUsage))
				}
				val, args = args[0], args[1:]
			}
			dbFile = val
			continue
		}
		return o.fail(fmt.Errorf("%w: unknown option %s", ErrUsage, args[0]))
	}
	if len(args) == 0 {
		return o.fail(fmt.Errorf("%w: no verb (fill, search, indexer, stats, forget, exclude, unexclude, pin, unpin, tag, untag, help)", ErrUsage))
	}
	if dbFile == "" {
		dbFile = DefaultDB()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "help":
		if len(rest) == 0 {
			help()
			return ExitOK
		}
		asJSON := false
		var topic string
		for _, a := range rest {
			if a == "-json" || a == "--json" {
				asJSON = true
			} else if topic == "" {
				topic = a
			} else {
				return o.fail(fmt.Errorf("%w: help takes one topic", ErrUsage))
			}
		}
		o.json = asJSON
		if topic == "" {
			help()
			return ExitOK
		}
		return o.fail(helpTopic(stdout, topic, asJSON))
	case "fill":
		return o.fail(o.fill(ctx, dbFile, rest))
	case "search":
		return o.fail(o.search(ctx, dbFile, rest))
	case "indexer":
		return o.fail(o.indexer(ctx, dbFile, rest))
	case "index":
		return o.fail(fmt.Errorf("%w: there is no index verb; did you mean indexer (build, validate, list)? search takes -index DIR", ErrUsage))
	case "stats":
		return o.fail(o.stats(ctx, dbFile, rest))
	case "forget":
		return o.fail(o.forget(ctx, dbFile, rest))
	case "exclude":
		return o.fail(o.exclude(ctx, dbFile, rest))
	case "unexclude":
		return o.fail(o.unexclude(ctx, dbFile, rest))
	case "pin":
		return o.fail(o.pin(ctx, dbFile, rest))
	case "unpin":
		return o.fail(o.unpin(ctx, dbFile, rest))
	case "tag":
		return o.fail(o.tag(ctx, dbFile, rest))
	case "untag":
		return o.fail(o.untag(ctx, dbFile, rest))
	}
	return o.fail(fmt.Errorf("%w: unknown verb %q (fill, search, indexer, stats, forget, exclude, unexclude, pin, unpin, tag, untag, help)", ErrUsage, verb))
}

// fail reports err (one line on standard error, and in JSON mode an object on
// standard output) and returns its exit code; nil is exit 0.
func (o *out) fail(err error) int {
	code, class := Classify(err)
	if err == nil || errors.Is(err, ErrNoMatch) {
		return code
	}
	fmt.Fprintf(o.stderr, "%s: %s\n", appName, strings.ReplaceAll(err.Error(), "\n", " "))
	if o.json {
		b, _ := json.Marshal(map[string]any{"error": err.Error(), "class": class, "code": code})
		fmt.Fprintf(o.stdout, "%s\n", b)
	}
	return code
}

// flags makes a FlagSet that reports its errors as usage errors, not output.
func flags(name string, o *out) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	j := fs.Bool("json", false, "")
	return fs, j
}

func parse(fs *flag.FlagSet, args []string, o *out, j *bool) error {
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %s %v", ErrUsage, fs.Name(), err)
	}
	o.json = *j
	return nil
}

// openExisting opens a corpus that must already exist.
func openExisting(path string) (*corpus.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no corpus at %s: %w", path, err)
	}
	return corpus.Open(path)
}

func (o *out) fill(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("fill", o)
	pause := fset.Duration("pause", time.Second, "")
	robots := fset.Bool("robots", true, "")
	ua := fset.String("user-agent", "", "")
	maxPages := fset.Int("max-pages", 0, "")
	depth := fset.Int("depth", 0, "")
	batch := fset.Int("batch", 0, "")
	every := fset.Duration("every", 0, "")
	untilDone := fset.Bool("until-done", false, "")
	every0 := false
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	fset.Visit(func(f *flag.Flag) { every0 = every0 || f.Name == "every" })
	if fset.NArg() != 1 {
		return fmt.Errorf("%w: fill takes one SOURCE, a folder or an http(s) address", ErrUsage)
	}
	if *maxPages < 0 || *depth < 0 || *pause < 0 || *batch < 0 {
		return fmt.Errorf("%w: -max-pages, -depth, -pause and -batch must not be negative", ErrUsage)
	}
	if every0 && (*every <= 0 || *batch == 0) {
		return fmt.Errorf("%w: -every needs a positive duration and -batch N", ErrUsage)
	}
	if *untilDone && !every0 {
		return fmt.Errorf("%w: -until-done goes with -every", ErrUsage)
	}
	src := fset.Arg(0)
	lim := toc.Limits{Pages: *maxPages, Depth: *depth}
	remote, tocURL, err := source(src)
	if err != nil {
		return err
	}
	if !remote && *batch > 0 {
		return fmt.Errorf("%w: -batch and -every are for web sites; a folder is read whole", ErrUsage)
	}
	if !remote {
		st, err := os.Stat(src)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("%w: %s is not a folder; fill takes a site's folder or an address", ErrUsage, src)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrCantCreate, err)
	}
	db, err := corpus.Open(dbFile)
	if err != nil {
		if errors.Is(err, corpus.ErrSchema) || errors.Is(err, corpus.ErrLocked) || errors.Is(err, fs.ErrPermission) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrCantCreate, err)
	}
	defer db.Close()
	if !remote {
		rep, err := db.FillLocal(ctx, src, lim)
		if rep != nil {
			o.report(rep, false)
		}
		return err
	}
	agent := *ua
	if agent == "" {
		agent = appName + "/" + cmarkfind.Version
	}
	f := fetch.New(fetch.Options{UserAgent: agent, Pause: *pause, Robots: *robots})
	once := func() (*corpus.FillReport, error) {
		rep, err := db.FillRemoteWith(ctx, f, tocURL, lim, corpus.FillOptions{Batch: *batch})
		if rep != nil {
			o.report(rep, *batch > 0)
		}
		return rep, err
	}
	if !every0 {
		_, err := once()
		return err
	}
	// -every: batch after batch until interrupted (or, with -until-done, until
	// nothing remains). A failed batch is reported and the loop goes on; the
	// end carries the first failure's class.
	var first error
	for {
		rep, err := once()
		if ctx.Err() != nil {
			return first
		}
		if err != nil {
			fmt.Fprintf(o.stderr, "%s: %s\n", appName, strings.ReplaceAll(err.Error(), "\n", " "))
			if first == nil {
				first = err
			}
		}
		if *untilDone && rep != nil && err == nil && rep.Remaining == 0 {
			return first
		}
		select {
		case <-ctx.Done():
			return first
		case <-time.After(*every):
		}
	}
}

// source says whether src names a site on the web, and gives the address of its
// outline; for a folder it gives the absolute path, which is how a local site
// is stored.
func source(src string) (remote bool, key string, err error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		u, err := url.Parse(src)
		if err != nil || u.Host == "" {
			return false, "", fmt.Errorf("%w: %q is not an address", ErrUsage, src)
		}
		if strings.HasSuffix(u.Path, ".opml") {
			return true, src, nil
		}
		return true, strings.TrimRight(src, "/") + "/.well-known/toc.opml", nil
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return false, "", fmt.Errorf("%w: %v", ErrUsage, err)
	}
	return false, abs, nil
}

func (o *out) report(r *corpus.FillReport, batched bool) {
	if o.json {
		b, _ := json.Marshal(map[string]any{"added": r.Added, "updated": r.Updated, "unchanged": r.Unchanged, "gone": r.Gone,
			"missing": r.Missing, "failed": r.Failed, "forbidden": r.Forbidden, "skipped": r.Skipped, "excluded": r.Excluded, "listed": r.Listed, "remaining": r.Remaining, "complete": r.Complete})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return
	}
	fmt.Fprintf(o.stdout, "added %d, updated %d, unchanged %d, gone %d, missing %d, failed %d, forbidden %d, skipped %d, excluded %d\n",
		r.Added, r.Updated, r.Unchanged, r.Gone, r.Missing, r.Failed, r.Forbidden, r.Skipped, r.Excluded)
	if batched {
		fmt.Fprintf(o.stdout, "listed %d, remaining %d\n", r.Listed, r.Remaining)
	}
	if !r.Complete {
		fmt.Fprintln(o.stdout, "the table of contents was cut short (a limit or a cancel): nothing was marked gone")
	}
}

// tagList is a repeatable -tag option.
type tagList []string

func (t *tagList) String() string     { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error { *t = append(*t, v); return nil }

func (o *out) search(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("search", o)
	limit := fset.Int("limit", 10, "")
	pinned := fset.Bool("pinned", false, "")
	idx := fset.String("index", "", "")
	var tags tagList
	fset.Var(&tags, "tag", "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if *limit < 1 {
		return fmt.Errorf("%w: -limit must be at least 1", ErrUsage)
	}
	if idxSet(fset) {
		if len(tags) > 0 || *pinned {
			return fmt.Errorf("%w: -tag and -pinned search the corpus, not an index", ErrUsage)
		}
		if *idx == "" {
			return fmt.Errorf("%w: -index needs a directory", ErrUsage)
		}
		return o.searchIndex(*idx, strings.Join(fset.Args(), " "), *limit)
	}
	if fset.NArg() == 0 && len(tags) == 0 && !*pinned {
		return fmt.Errorf("%w: search needs a query, or -tag or -pinned", ErrUsage)
	}
	if *limit < 1 {
		return fmt.Errorf("%w: -limit must be at least 1", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := db.SearchWith(ctx, strings.Join(fset.Args(), " "), corpus.SearchOptions{Limit: *limit, Tags: tags, Pinned: *pinned})
	if err != nil {
		return err
	}
	filtered := len(tags) > 0 || *pinned
	if o.json {
		hits := make([]map[string]any, 0, len(res.Hits))
		for _, h := range res.Hits {
			ts := h.Tags
			if ts == nil {
				ts = []string{}
			}
			hits = append(hits, map[string]any{"address": h.Address, "title": h.Title, "site": h.Site, "tier": h.Tier, "snippet": h.Snippet, "pinned": h.Pinned, "tags": ts})
		}
		m := map[string]any{"hits": hits, "searched": res.Searched, "pending": res.Pending}
		if filtered {
			m["filtered"] = res.Filtered
		}
		b, _ := json.Marshal(m)
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		for _, h := range res.Hits {
			marks := ""
			if h.Pinned {
				marks += "  pinned"
			}
			if len(h.Tags) > 0 {
				marks += "  tags: " + strings.Join(h.Tags, " ")
			}
			fmt.Fprintf(o.stdout, "%s\n  %s  [%s]%s\n", h.Title, h.Address, h.Tier, marks)
			if h.Snippet != "" {
				fmt.Fprintf(o.stdout, "  %s\n", strings.Join(strings.Fields(h.Snippet), " "))
			}
		}
		n := len(res.Hits)
		more := ""
		if filtered {
			what := "with the tag"
			switch {
			case *pinned && len(tags) > 0:
				what = "pinned with the tag"
			case *pinned:
				what = "pinned"
			case len(tags) > 1:
				what = "with the tags"
			}
			more = fmt.Sprintf(", %d %s", res.Filtered, what)
		}
		if res.Pending > 0 {
			more += fmt.Sprintf("; %d listed pages not fetched yet", res.Pending)
		}
		fmt.Fprintf(o.stderr, "%s: %d %s, searched %d pages%s\n", appName, n, plural(n, "hit", "hits"), res.Searched, more)
	}
	if len(res.Hits) == 0 {
		return ErrNoMatch
	}
	return nil
}

func idxSet(fset *flag.FlagSet) bool {
	set := false
	fset.Visit(func(f *flag.Flag) { set = set || f.Name == "index" })
	return set
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (o *out) stats(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("stats", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 0 {
		return fmt.Errorf("%w: stats takes no arguments", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := db.Stats(ctx)
	if err != nil {
		return err
	}
	var size int64
	if fi, err := os.Stat(dbFile); err == nil {
		size = fi.Size()
	}
	if o.json {
		sites := make([]map[string]any, 0, len(st.Sites))
		for _, s := range st.Sites {
			sites = append(sites, map[string]any{"site": s.Site, "tier": s.Tier, "pages": s.Pages, "gone": s.Gone, "listed": s.Listed, "pending": s.Pending})
		}
		b, _ := json.Marshal(map[string]any{"path": dbFile, "schema_version": st.SchemaVersion, "bytes": size, "pages": st.Pages, "gone": st.Gone, "excluded": st.Excluded, "pinned": st.Pinned, "tags": st.Tags, "tag_pages": st.TagPages, "pending": st.Pending, "sites": sites})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "path %s\nschema %d\nsize %d bytes\npages %d\ngone %d\nexcluded %d\npinned %d\ntags %d (%d on pages)\npending %d\n", dbFile, st.SchemaVersion, size, st.Pages, st.Gone, st.Excluded, st.Pinned, st.Tags, st.TagPages, st.Pending)
	for _, s := range st.Sites {
		more := ""
		if s.Pending > 0 {
			more = fmt.Sprintf(", %d of %d listed not fetched yet", s.Pending, s.Listed)
		}
		fmt.Fprintf(o.stdout, "%s  %d pages, %d gone%s  %s\n", s.Tier, s.Pages, s.Gone, more, s.Site)
	}
	return nil
}

// note says something about a negative answer on standard error, not as an error.
func (o *out) note(format string, a ...any) {
	fmt.Fprintf(o.stderr, "%s: %s\n", appName, fmt.Sprintf(format, a...))
}

func (o *out) forget(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("forget", o)
	all := fset.Bool("all", false, "")
	dry := fset.Bool("dry-run", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if (*all && fset.NArg() != 0) || (!*all && fset.NArg() != 1) {
		return fmt.Errorf("%w: forget takes one SITE, or -all", ErrUsage)
	}
	site := ""
	if !*all {
		_, key, err := source(fset.Arg(0))
		if err != nil {
			return err
		}
		site = key
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	switch {
	case *dry && *all:
		st, serr := db.Stats(ctx)
		if serr != nil {
			return serr
		}
		n = st.Pages + st.Gone
	case *dry:
		n, err = db.CountSite(ctx, site)
	case *all:
		n, err = db.ForgetAll(ctx)
	default:
		n, err = db.Forget(ctx, site)
	}
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"forgotten": n, "dry_run": *dry})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else if *dry {
		fmt.Fprintf(o.stdout, "would forget %d %s\n", n, plural(n, "page", "pages"))
	} else {
		fmt.Fprintf(o.stdout, "forgot %d %s\n", n, plural(n, "page", "pages"))
	}
	if n == 0 && !*all {
		o.note("no pages from %s", site)
		return ErrNoMatch
	}
	return nil
}

func (o *out) exclude(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("exclude", o)
	list := fset.Bool("list", false, "")
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if (*list && fset.NArg() != 0) || (!*list && fset.NArg() != 1) {
		return fmt.Errorf("%w: exclude takes one PREFIX, or -list", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	if *list {
		rules, err := db.Excluded(ctx)
		if err != nil {
			return err
		}
		if o.json {
			out := make([]map[string]string, 0, len(rules))
			for _, r := range rules {
				out = append(out, map[string]string{"prefix": r.Prefix, "added_at": r.AddedAt})
			}
			b, _ := json.Marshal(map[string]any{"excluded": out})
			fmt.Fprintf(o.stdout, "%s\n", b)
			return nil
		}
		for _, r := range rules {
			fmt.Fprintf(o.stdout, "%s  %s\n", r.Prefix, r.AddedAt)
		}
		return nil
	}
	n, err := db.Exclude(ctx, fset.Arg(0))
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"prefix": fset.Arg(0), "removed": n})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "excluded %s; removed %d stored %s\n", fset.Arg(0), n, plural(n, "page", "pages"))
	}
	return nil
}

func (o *out) unexclude(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("unexclude", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		return fmt.Errorf("%w: unexclude takes one PREFIX", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	ok, err := db.Unexclude(ctx, fset.Arg(0))
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"prefix": fset.Arg(0), "removed": ok})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else if ok {
		fmt.Fprintf(o.stdout, "no longer excluded: %s\n", fset.Arg(0))
	}
	if !ok {
		o.note("%s is not excluded", fset.Arg(0))
		return ErrNoMatch
	}
	return nil
}
