package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/rsdoiel/cmarkfind"
)

/** Topic is one subject of `cmarkfind help`: a manual page of section 7,
 * cmarkfind-NAME(7).
 *
 * Fields:
 *   Name (string)    — the topic and the last part of the page's name
 *   Summary (string) — one line, shown by `help index` and as the page's NAME
 *   Body (string)    — the page after NAME, in Markdown, with # headings
 *
 * Example:
 *   for _, t := range cli.Topics { fmt.Println(t.Name, t.Summary) }
 */
type Topic struct {
	Name, Summary, Body string
}

/** Topics lists the help topics in the order `help index` shows them: the
 * verbs, then the subjects that belong to no one verb. The Makefile's TOPICS
 * line names the same set (a test checks it).
 *
 * Example:
 *   n := len(cli.Topics)
 */
var Topics = []Topic{
	{"fill", "read a site's pages into the corpus", `# SYNOPSIS

cmarkfind fill [OPTIONS] SOURCE

# DESCRIPTION

Read the pages a site's table of contents (/.well-known/toc.opml) lists and
store them. SOURCE is a folder (a site on disk, nothing is fetched) or an
http:// or https:// address, either the site's address or its toc.opml.

Pages already stored are asked for again only if they changed: the web fill
sends If-None-Match and takes a 304 as unchanged. A page the table of contents
no longer lists is marked gone and is not searched. A page that is excluded (see
cmarkfind-exclude(7)) is neither stored nor requested.

The web fill is polite. It waits between requests to one host, names itself in
the User-Agent, and obeys robots.txt.

A big site can be read a few pages at a time. With -batch N a fill asks about at
most N pages, never-asked pages first, then the ones checked longest ago, and
says how many remain. Run it again to continue; with -every it does so by
itself.

# OPTIONS

-pause DURATION
: Wait between requests to one host. Default 1s.

-robots
: Obey robots.txt. Default true; -robots=false ignores it.

-user-agent TEXT
: The User-Agent sent. Default cmarkfind/VERSION.

-max-pages N
: List at most N pages from the table of contents.

-depth N
: Follow included outlines N levels.

-batch N
: Ask about at most N pages, then stop (web sites only).

-every DURATION
: With -batch, run another batch every DURATION until interrupted.

-until-done
: With -every, stop when no listed page is unasked.

-json
: Write the report as JSON.

# EXAMPLES

    cmarkfind fill ~/Sites/example.org
    cmarkfind fill https://rsdoiel.github.io
    cmarkfind fill -batch 25 -every 1h -until-done https://example.org

# EXIT STATUS

See cmarkfind-exit-status(7). A fill in which some pages failed finishes, prints
its counts, and exits with the class of the first failure.

# SEE ALSO

cmarkfind(1), cmarkfind-exclude(7), cmarkfind-forget(7), cmarkfind-corpus(7)
`},
	{"search", "search the corpus", `# SYNOPSIS

cmarkfind search [OPTIONS] QUERY...

# DESCRIPTION

Search the corpus and print the hits as a Markdown list: each is a link
(title as the text, address as the target, with #heading added when the match is
under a heading below the page's title), then a line with its tier, whether
it is pinned and its tags, then the matching text with the matched words in
**bold**. The output can be saved as a .md file or read in cmarkbrowser. -json
has the same facts for programs. The count and how many pages were searched go to standard error.
QUERY is words, "phrases", AND, OR, NOT, -word, word* and title:word; see
cmarkfind-query(7). Text that is not valid query syntax is searched as plain
words. The search reads the index that the last fill built beside the corpus
(cmarkfind-indexer(7)); when it is missing or is not the corpus's own, search
builds it first, which takes a few seconds on a large corpus.

# OPTIONS

-limit N
: At most N hits. Default 10.

-index DIR
: Search the index in DIR (see cmarkfind-indexer(7)) instead of the corpus's
  own index; the pins, tags and tiers of the corpus do not apply, so -tag and
  -pinned do not either. Exit 66 when DIR holds no index, 65 when it is damaged,
  2 when the query is too large.

-json
: Write {"hits": [...], "searched": N}; each hit has "anchor", the heading slug
  (empty when there is none). With -index each hit also has "excerpt"
  (the best 30 words, HTML-escaped, each matched word in <mark>) and, when the
  excerpt sits under a heading, "anchor" (the heading's slug, for address#anchor).

Use -- before a query that starts with a dash.

# EXIT STATUS

1 when nothing matches, 66 when there is no corpus yet. See
cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind search oberon compiler
    cmarkfind search -json '"shallow water"' | jq .

# SEE ALSO

cmarkfind(1), cmarkfind-query(7), cmarkfind-corpus(7)
`},
	{"indexer", "build, check and list a search index", `# SYNOPSIS

cmarkfind indexer build [-chunk N] [DIR]

cmarkfind indexer validate [DIR]

cmarkfind indexer [list] [DIR]

cmarkfind search -index DIR QUERY...

# DESCRIPTION

An index is a folder of compressed files, made from the corpus, that a program
can search without the corpus (and, later, over HTTP). build writes one from
every page the corpus holds that is not gone and not under an excluded prefix.
Building again replaces it: the generation number grows by one and files the
new manifest no longer names are removed once they are ten minutes old, so
a search that began before the rebuild can finish. The same pages always give the same
files.

validate reads every file and checks it against the digest in manifest.json.
list says what an index holds: pages, chunks, generation and when it was built.

DIR is optional. Without it the CMARKFIND_INDEX variable is used, else a folder
named index beside the corpus file (so -db moves both). The corpus builds its own index in that folder after every fill, forget or
exclude that changed pages, and plain search reads it; a search finding it
missing, or built by something else, builds it first. indexer build rebuilds it
by hand, and search -index DIR searches any other index.

# OPTIONS

-chunk N
: Entries per word file when building. Default 10000.

-json
: Write the result as JSON.

# EXIT STATUS

66 when DIR holds no index (or, for build, there is no corpus), 65 when an index
is damaged, 73 when DIR cannot be created. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind indexer build
    cmarkfind indexer validate
    cmarkfind search -index ~/.local/share/cmarkfind/index 'title:otter OR "shallow water"'

# SEE ALSO

cmarkfind(1), cmarkfind-search(7), cmarkfind-query(7), cmarkfind-corpus(7)
`},
	{"stats", "say what the corpus holds", `# SYNOPSIS

cmarkfind stats [OPTIONS]

# DESCRIPTION

Say what the corpus holds: pages, pages gone, counts by site and tier, how many
listed pages have not been fetched yet, the file's size and its schema version.

# OPTIONS

-json
: Write the result as JSON.

# EXIT STATUS

66 when there is no corpus yet. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
`},
	{"forget", "delete a site's pages, or every page", `# SYNOPSIS

cmarkfind forget [OPTIONS] SITE

cmarkfind forget -all

# DESCRIPTION

Delete the pages stored from one site (a folder or an address, named as for
cmarkfind-fill(7)), or every page. Exclusions are kept, so a later fill does
not bring back what exclude keeps out.

# OPTIONS

-all
: Forget every page.

-dry-run
: Say how many pages would go; delete nothing.

-json
: Write {"forgotten": N, "dry_run": false}.

# EXIT STATUS

1 when SITE stored nothing. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind forget -dry-run https://example.org
    cmarkfind forget ~/Sites/example.org

# SEE ALSO

cmarkfind(1), cmarkfind-exclude(7), cmarkfind-fill(7)
`},
	{"exclude", "keep an address prefix out of the corpus", `# SYNOPSIS

cmarkfind exclude [OPTIONS] PREFIX

cmarkfind exclude -list

# DESCRIPTION

Keep every page whose address starts with PREFIX out of the corpus. Stored pages
under it are deleted; later fills skip them and do not request them.

PREFIX is an http:// or https:// address, matched as plain text, so end a folder
with a slash: https://example.org/drafts/ (without the slash,
https://example.org/drafts-old matches too).

# OPTIONS

-list
: Show the rules.

-json
: Write the result as JSON.

# EXAMPLES

    cmarkfind exclude https://example.org/drafts/
    cmarkfind exclude -list

# SEE ALSO

cmarkfind(1), cmarkfind-unexclude(7), cmarkfind-forget(7), cmarkfind-fill(7)
`},
	{"unexclude", "remove an exclusion", `# SYNOPSIS

cmarkfind unexclude PREFIX

# DESCRIPTION

Remove an exclusion. The pages it kept out come back with the next fill.

# OPTIONS

-json
: Write {"prefix": ..., "removed": true|false}.

# EXIT STATUS

1 when there was no such rule. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-exclude(7)
`},
	{"pin", "keep a page near the top of the results", `# SYNOPSIS

cmarkfind pin ADDRESS...

cmarkfind pin -list

# DESCRIPTION

Pin pages. A pinned page that matches a search is listed before the other
matches; among the pinned ones, and among the others, the order is as before.
A pin never makes a page match, and a page marked gone is not searched.

ADDRESS is the page's published address as search shows it, an http:// or
https:// address, matched as written. Pinning twice does nothing. An address the
corpus does not hold may be pinned (the page may be filled later); that is
noted on standard error. Pins are kept by forget and by a refill.

# OPTIONS

-list
: Show the pinned addresses with their titles, and whether each page is stored,
  gone or not in the corpus.

-json
: Write the result as JSON.

# EXAMPLES

    cmarkfind pin https://example.org/oberon.md
    cmarkfind pin -list
    cmarkfind search -pinned compiler

# SEE ALSO

cmarkfind(1), cmarkfind-unpin(7), cmarkfind-search(7), cmarkfind-tag(7)
`},
	{"unpin", "remove a pin", `# SYNOPSIS

cmarkfind unpin ADDRESS...

# DESCRIPTION

Remove pins. The pages stay in the corpus.

# OPTIONS

-json
: Write {"unpinned": N}.

# EXIT STATUS

1 when none of the addresses was pinned. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-pin(7)
`},
	{"tag", "give a page your own tags", `# SYNOPSIS

cmarkfind tag ADDRESS TAG...

cmarkfind tag -list [ADDRESS]

# DESCRIPTION

Give a page tags, your own words for what it is about. A tag is 1 to 64 letters,
digits, - or _ and is folded to lower case. Tags are not part of the page text:
a plain word does not match a tag. Find pages by tag with
cmarkfind search -tag TAG; give -tag more than once and a page must carry every
tag. Tags are kept by forget and by a refill. Front matter tags are not read.

# OPTIONS

-list
: Show every tag with the number of pages that carry it, or with an ADDRESS,
  that page's tags.

-json
: Write the result as JSON.

# EXAMPLES

    cmarkfind tag https://example.org/oberon.md compilers history
    cmarkfind search -tag compilers
    cmarkfind search -tag compilers -tag history oberon

# SEE ALSO

cmarkfind(1), cmarkfind-untag(7), cmarkfind-search(7), cmarkfind-pin(7)
`},
	{"untag", "take tags off a page", `# SYNOPSIS

cmarkfind untag ADDRESS TAG...

# DESCRIPTION

Take tags off a page.

# OPTIONS

-json
: Write {"address": ..., "removed": N}.

# EXIT STATUS

1 when the page carried none of the tags. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-tag(7)
`},
	{"query", "the search syntax", `# DESCRIPTION

A query is a Lucene-style string, searched over each page's title, description
and body.

    oberon compiler          both words (AND is implied)
    oberon OR modula         either word
    oberon NOT pascal        the first and not the second (-pascal is the same)
    "shallow water"          the words next to each other, in order
    compil*                  words that start with compil
    title:oberon             the word in the title only (also description:, body:)
    (oberon OR modula) -tide groups

Case does not matter. A title match counts for more than a description match,
and a description match for more than one in the body (bm25 with weights 4, 2
and 1). Hits that rank the same are put in the order bookmarked, read, own, toc
pages, then by address.

Text that is not valid query syntax (an unbalanced quote, a stray operator) is
searched as plain words, so a typo returns hits and not an error; only a query
over the size limit is refused (exit 2). Words of another search engine's
syntax that this one does not have, such as NEAR(a b), ^word, a "phrase"~3 or
fuzzy word~, are read as the words they are made of. Pages marked gone are not
searched. The text shown with a hit is the 14 words of the page that hold the
most matches, each match in [brackets].

# EXAMPLES

    cmarkfind search '"shallow water" NOT tide'
    cmarkfind search -- -dash

# SEE ALSO

cmarkfind-search(7), cmarkfind-corpus(7)
`},
	{"corpus", "the file cmarkfind keeps", `# DESCRIPTION

The corpus is one SQLite3 file. It holds, for each page, its address, site,
title, description, date, body, a hash of its content, the ETag and
Last-Modified the site sent, when it was fetched and last checked, its tier, and
when it was found gone. A search index over title, description and body does the
searching; it is built from the pages after each fill and kept in a folder
beside the file (cmarkfind-indexer(7)). Nothing leaves the machine when you search.

The file is named, in this order, by -db FILE, by the CMARKFIND_DB variable, or
it is cmarkfind/corpus.db in your user data directory. fill creates it; search
and stats need it to exist. Several programs may open it at once (WAL mode), and
cmarkbrowser searches and fills the same file.

The tier says how a page came in: own (a site on disk), toc (a table of contents
read over the web), read (a page you read in cmarkbrowser) or bookmarked. It
breaks ties between hits that rank the same.

The file carries a schema version. A file from an older cmarkfind is upgraded in
place (schema 5 drops the old FTS5 table; the file keeps its size until you run
VACUUM on it); a file from a newer one is refused (exit 65), and the file is not
changed.

Page text comes from sites you do not control. Search results show it as text
only: no link, image, HTML or terminal hyperlink in it is made live.

# SEE ALSO

cmarkfind-fill(7), cmarkfind-search(7), cmarkfind-stats(7), cmarkfind-forget(7)
`},
	{"exit-status", "what the exit codes mean", `# DESCRIPTION

cmarkfind uses the exit codes of the workspace convention. With -json a failure
is also written to standard output as {"error": "...", "class": "...", "code": N}.

# EXIT STATUS

0
: Success.

1
: The answer is no: search found nothing (also over an index); forget's SITE or unexclude's PREFIX
  had nothing to act on; robots.txt forbids the table of contents.

2
: Bad command line: unknown verb, option or topic, missing argument, bad value;
  a query is too large (over 1,024 bytes, 64 clauses or 8 levels of parentheses).

65
: The file is not a corpus or has a newer schema; a source is malformed; an
  index is damaged (a file does not match its digest, or the manifest is wrong).

66
: A folder, the corpus (for search and stats) or a table of contents is missing;
  an index is missing (no manifest.json in its folder).

69
: The site cannot be reached.

70
: A bug.

73
: The corpus file or its directory, or an index's directory, cannot be created.

75
: The corpus is locked by another program; try again.

77
: The system refused access.

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
`},
}

// topicPage is the section 7 page for t, with its placeholders filled.
func topicPage(t Topic) string {
	src := fmt.Sprintf("---\ntitle: {app_name}-%s\nsection: 7\nheader: user manual\nfooter: version {version} {release_hash}\ndate: {release_date}\nauthor: R. S. Doiel\n---\n\n# NAME\n\n{app_name}-%s — %s\n\n%s",
		t.Name, t.Name, t.Summary, t.Body)
	return cmarkfind.FmtHelp(src, appName, cmarkfind.Version, cmarkfind.ReleaseDate, cmarkfind.ReleaseHash)
}

// helpTopic writes the page for name, or the index for "index". An unknown
// topic is a usage error that names the index.
func helpTopic(w io.Writer, name string, asJSON bool) error {
	if name == "index" {
		if asJSON {
			type item struct {
				Name    string `json:"name"`
				Summary string `json:"summary"`
			}
			var list []item
			for _, t := range Topics {
				list = append(list, item{t.Name, t.Summary})
			}
			b, _ := json.Marshal(map[string]any{"topics": list})
			fmt.Fprintf(w, "%s\n", b)
			return nil
		}
		width := 0
		for _, t := range Topics {
			width = max(width, len(t.Name))
		}
		for _, t := range Topics {
			fmt.Fprintf(w, "%-*s  %s\n", width, t.Name, t.Summary)
		}
		return nil
	}
	for _, t := range Topics {
		if t.Name == name {
			fmt.Fprint(w, topicPage(t))
			return nil
		}
	}
	return fmt.Errorf("%w: no help for %q (cmarkfind help index lists the topics)", ErrUsage, name)
}
