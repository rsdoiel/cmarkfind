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

-site-url ADDRESS
: Folders only. Store every page under this published address (scheme, host and
  path prefix) whatever the outline lists, keeping each page's own path. Needs
  an http or https address with no query or fragment; exit 2 otherwise, or with a
  web site. Filling again with another address marks the old ones gone.

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
list says what an index holds: pages, chunks, generation and when it was built,
and the words it holds without positions.

A word on a great many pages (a very common word such as "the", in any
language) can make its entry too big for one index file. Over about 2 MiB an
entry is written without positions: the index still knows which pages hold the
word and in which of title, description and body, and not where. A search for
the word, a prefix, a field clause and AND, OR and NOT work as before. A phrase
through such a word is checked against the text of each hit, so the answer is
exact; a query for a phrase through one reads the pages it shows, and the pages
above them that fail the check, and a limit keeps that small. The word is marked
in a snippet from the page's text. build
prints these words with their page and position counts, list prints the words, and a
fill, forget or exclude that rebuilds warns on standard error when the set of
words has changed. A word of more than 128 bytes (a hash, a pasted blob) is not
indexed at all. A build that still cannot fit a file names the word and exits
73; the index already in place is left as it was and keeps answering searches.

A word that is on more than a million pages is more than a reader will take (a
posting list is one array, and a reader refuses one of over a million elements),
so the build stops, names the word and its page count, and exits 73 instead of
writing an index every search would call damaged; the index already in place is
kept. A corpus of that size is far beyond what a rebuild after every fill suits.

A page whose own text is over one index file (about two million Chinese or
Japanese characters in a page) cannot be written. The build leaves that page out,
warns on standard error with its address, and goes on; the page stays in the
corpus and is not searchable. stats lists such pages as "not indexed".

DIR is optional. Without it the CMARKFIND_INDEX variable is used, else a folder
named index beside the corpus file (so -db moves both). The corpus builds its own index in that folder after every fill, forget or
exclude that changed pages, and plain search reads it; a search finding it
missing, or built by something else, builds it first. indexer build rebuilds it
by hand, and search -index DIR searches any other index.

The folder is the corpus's own: the first corpus to build into it writes a small
file, .cmarkfind-owner, naming itself, and a second corpus pointed at the same
folder is refused (exit 78) instead of rebuilding over the first on every
search. An index with no owner file (built by an older cmarkfind) goes to the
first corpus that builds it. indexer build DIR into any other folder does not
claim it.

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
listed pages have not been fetched yet, the file's size and its schema version,
and, when there are any, the very common words the index holds without
positions (see cmarkfind-indexer(7)); JSON has them as "document_only". A page
whose text is too big for one index file (about two million Chinese or Japanese
characters) is stored but left out of the index: a "not indexed" line names it
(JSON: "not_indexed", a list, empty when nothing is missing) until the page is
excluded, forgotten or replaced by a smaller one.

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
	{"verbs", "list the verbs and their options", `# SYNOPSIS

cmarkfind verbs [-json]

# DESCRIPTION

List every verb with a one-line summary. With -json the output is
{"verbs": [{"name", "summary", "subverbs", "flags"}]}: the flags of each verb,
each with its name and whether it is a switch ("bool": true) or takes a value.
A verb with subverbs (indexer) lists the flags of each under "sub". The lists
are read from the verbs themselves, the ones shell completion uses. It opens no
corpus.

# OPTIONS

-json
: Write the list as JSON.

# EXIT STATUS

0
: Success.

2
: An argument or an unknown option.

See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-completion(7)
`},
	{"completion", "set up tab completion in bash or PowerShell", `# SYNOPSIS

cmarkfind completion bash|powershell

cmarkfind completion bash|powershell -install | -uninstall | -check

# DESCRIPTION

Write a completion script for a shell. It completes the verbs, indexer's
subverbs, each verb's options, the help topics, and file names where a file is
wanted (-db, -index and the arguments of fill and indexer). The lists come from
the verbs themselves (see cmarkfind-verbs(7)), so a new verb or option is
completed with no change to the script generator.

With no option the script goes to standard output. Load it for one session:

    eval "$(cmarkfind completion bash)"
    cmarkfind completion powershell | Out-String | Invoke-Expression

-install does that for every new session. For bash it writes
$XDG_DATA_HOME/bash-completion/completions/cmarkfind (under ~/.local/share when
XDG_DATA_HOME is not set) and adds one line to the file a new shell reads,
~/.bashrc, or on macOS the first of ~/.bash_profile, ~/.bash_login and
~/.profile that exists; the line is what makes it work where no loader reads the
completions directory, as in macOS's bash 3.2. For PowerShell it writes
cmarkfind-completion.ps1 beside the profile (~/.config/powershell, or
Documents\PowerShell on Windows) and adds one line to Microsoft.PowerShell_profile.ps1.
The line is added once. An existing file that cmarkfind did not write is never
overwritten.

# OPTIONS

-install
: Write the script and have the shell load it.

-uninstall
: Remove the script if cmarkfind wrote it, and the line it added. Files
  cmarkfind did not write are left alone.

-check
: Say, one line each, whether the script is installed, current, and sourced by
  the startup file. It reads files only; for bash it ends with the command that
  asks the running shell.

-json
: Write the result as JSON: {"shell", "path"} for -install, {"shell", "removed"}
  for -uninstall and {"shell", "ok"} for -check.

# EXIT STATUS

0
: Done, or -check found everything in order.

1
: -uninstall found nothing of cmarkfind's to remove; -check found a problem.

2
: No shell, a shell other than bash or powershell (pwsh is accepted), or more
  than one of -install, -uninstall and -check.

66
: No home directory could be found.

73
: A file that cmarkfind did not write is in the way, or the script's directory
  cannot be made.

74
: The startup file could not be read or written.

77
: The system refused access.

See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-verbs(7)
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

Case does not matter, nor does the spelling of an accent or a letter form: an
accent written as a combining mark, a ligature, a full-width letter and the
German sharp s match their plain forms (accents are not removed).

Chinese and Japanese are written without spaces, so each Han, Hiragana, Katakana
or Bopomofo character is a word of its own and a query of several is the phrase
of them: 東京都 finds the pages that have those three characters in a row, and
that includes 東京都 inside a longer run of text. There is no dictionary; a
search finds the characters you wrote, in order, and does not know where the
words of a sentence begin and end. Korean is written with spaces and its words
are indexed as written, with the particle attached (한국어를). A Korean word of
two or more syllables in a query is therefore read as a prefix: 한국어 finds
한국어를, 한국어는 and 한국어. A word in quotation marks is not (the phrase "한국어"
finds only the word 한국어), and a single syllable is not either. Thai, Lao,
Khmer and Burmese are not segmented: a run of letters between spaces is one
word, so a word in the middle of a clause cannot be found, only the start of
it with a prefix (ภาษา*).

A title match counts for more than a description match,
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
  had nothing to act on; robots.txt forbids the table of contents; completion
  -uninstall found nothing, or -check found a problem.

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
: The corpus file or its directory, or an index's directory, cannot be created;
  completion's script or its directory cannot be created, or a file cmarkfind did
  not write is in the way.

74
: A read or write failed part way: completion's startup file.

75
: The corpus is locked by another program; try again.

77
: The system refused access.

78
: The index folder (CMARKFIND_INDEX, or the folder beside the corpus) belongs to
  another corpus. Give each corpus a folder of its own.

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
