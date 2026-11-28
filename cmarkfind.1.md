---
title: cmarkfind
section: 1
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind — a personal search engine for the Markdown web

# SYNOPSIS

cmarkfind [-db FILE] VERB [OPTIONS] ARGS

cmarkfind -help | -version | -license

# DESCRIPTION

cmarkfind keeps a corpus of Markdown pages in one SQLite file and searches it
locally. It fills the corpus from a site's table of contents
(/.well-known/toc.opml), from a folder on disk or over the web, builds a search
index for each site (an index set) after each fill, and searches those indexes with a
Lucene-style query language. Nothing is sent anywhere by a search.

# VERBS

fill [OPTIONS] SOURCE
: Read the pages a site's table of contents lists and store them. SOURCE is a
  folder (a site on disk, nothing fetched) or an http:// or https:// address
  (the site's address, or its toc.opml). Pages already stored are asked for
  again only if they changed. A page the table of contents no longer lists is
  marked gone and is not searched. Options: -pause DURATION (between requests
  to one host, default 1s), -robots (obey robots.txt, default true), -user-agent
  TEXT, -max-pages N, -depth N (how many levels of included outlines), -json.
  -name NAME reads a folder of Markdown files with no outline as a collection,
  stored under collection://NAME/path. -site-url ADDRESS (a folder only) stores every page under the address the site
  is published at, such as https://example.org/ or https://example.org/blog/,
  whatever the outline lists; without it a relative link in the outline is
  stored under http://site.local/ and fill warns.
  A big site can be read a few pages at a time: -batch N asks about at most N
  pages (never asked ones first, then the ones checked longest ago) and says
  how many remain; run it again to continue. -every DURATION (with -batch)
  keeps going, a batch every DURATION, until interrupted, or with -until-done
  until nothing remains. A search says how many listed pages are not fetched yet.

search [OPTIONS] QUERY...
: Search every index set that is not ignored and merge the answers. QUERY is words, "phrases", AND, OR, NOT and word*; text
  that is not valid query syntax is searched as plain words. Prints each hit's
  title, address and tier, and the matching text; the count and how many pages
  were searched go to standard error. Options: -limit N (default 10), -tag TAG
  (repeatable; a page must carry every tag), -pinned (only pinned pages), -json.
  With -tag or -pinned the query may be left out: the pages that qualify are
  listed by title. Pinned matches are listed first.
  Use -- before a query that starts with a dash.

search-set [OPTIONS] SET QUERY...
: Search one index set, ignored or not; options as search. See
  cmarkfind-search-set(7).

set list | show SET | ignore SET | unignore SET | fields [-reset] SET [FIELD=ROLE...]
: An index set is one instance of a site (an outline on the web, or a folder),
  made by fill and removed by forget, with an index of its own. list names the
  sets, show says what one holds, ignore leaves one out of search (search-set
  still reaches it), fields shows or sets how it reads front matter. See
  cmarkfind-set(7).

pin ADDRESS... | pin -list, unpin ADDRESS...
: Pin pages so they are listed first among the matches of a search, or take the
  pin off. See cmarkfind-pin(7).

tag ADDRESS TAG... | tag -list [ADDRESS], untag ADDRESS TAG...
: Give a page your own tags, or take them off. See cmarkfind-tag(7).

indexer build [SET] | indexer validate [SET] | indexer [list] [SET]
indexer discover SITE | indexer add [-reference] SITE | indexer refresh [SET]
: Write the search index of each set that needs one (or of SET), check one, or
  say what one holds. The indexes are kept in CMARKFIND_INDEX, else an index
  folder beside the corpus. discover finds the index a site publishes, add keeps
  a copy of it (or, with -reference, only its manifest) as a set, and refresh
  reads its manifest again. See cmarkfind-indexer(7).

server | server port N | server set NAME VALUE | server show
: Run a search service for this machine only (127.0.0.1): Markdown, HTML and
  JSON results with paging, an OpenSearch description for a browser's search
  box, and, unless turned off, version, stats and the list of sets. port N runs
  it once on one port; set and show keep its settings (port, concurrency,
  results, css, info) in the corpus. See cmarkfind-server(7).

stats [OPTIONS]
: Say what the corpus holds: pages, pages gone, counts by site and tier, the
  file's size and schema version. Option: -json.

forget [OPTIONS] SITE
forget -all
: Delete the pages stored from one site (named as for fill), or every page.
  Exclusions are kept. -dry-run says how many pages would go and deletes
  nothing. Exit 1 when SITE stored nothing.

exclude [OPTIONS] PREFIX
exclude -list
: Keep every page whose address starts with PREFIX out of the corpus: stored
  pages under it are deleted and later fills skip (and do not request) them.
  PREFIX is an http:// or https:// address, matched as plain text; end a folder
  with a slash. -list shows the rules.

unexclude PREFIX
: Remove an exclusion. Pages come back with the next fill. Exit 1 if there was
  no such rule.

verbs [-json]
: List the verbs with a line each; with -json also their options. See
  cmarkfind-verbs(7).

completion bash|powershell [-install | -uninstall | -check]
: Write a tab-completion script for bash or PowerShell, or install it, remove it
  or check it. See cmarkfind-completion(7).

help [TOPIC]
: Show this page, or one topic's page. help index lists the topics: one for
  each verb, and query, corpus and exit-status. Each is also a manual page of
  section 7, cmarkfind-TOPIC(7).

# OPTIONS

-db FILE
: The corpus file. Default: the CMARKFIND_DB variable, else cmarkfind/corpus.db
  in the user's data directory. fill creates it; search and stats need it.

-help, -version, -license
: Show the help, the version or the license.

-json
: On every verb, write the result as JSON on standard output. A failure is
  then also written there as {"error": "...", "class": "...", "code": N}.

# EXIT STATUS

0
: Success.

1
: search or search-set found nothing, or every set is ignored; forget's SITE or unexclude's PREFIX had nothing to
  act on; robots.txt forbids the table of contents; completion -uninstall found
  nothing, or -check found a problem.

2
: Bad command line: unknown verb or option, missing argument, bad value; a
  query is too large.

65
: The file is not a corpus or has a newer schema; a source is malformed; an
  index is damaged.

66
: A folder, the corpus (for search and stats), a table of contents, an index
  or a set is missing.

69
: The site cannot be reached. A fill in which some pages failed finishes, prints
  its counts and exits with the class of the first failure.

73
: The corpus file or its directory cannot be created; completion cannot write its
  script.

74
: A read or write failed part way (completion's startup file).

75
: The corpus is locked by another program; try again.

77
: The system refused access.

78
: The index folder belongs to another corpus; give each corpus its own
  (CMARKFIND_INDEX).

70
: A bug.

# EXAMPLES

    cmarkfind fill ~/Sites/example.org
    cmarkfind fill https://rsdoiel.github.io
    cmarkfind search oberon compiler
    cmarkfind search -json '"shallow water"' | jq .
    cmarkfind stats
    cmarkfind exclude https://example.org/drafts/
    cmarkfind forget ~/Sites/example.org

# SEE ALSO

cmarkfind-fill(7), cmarkfind-search(7), cmarkfind-search-set(7), cmarkfind-set(7), cmarkfind-indexer(7), cmarkfind-server(7), cmarkfind-stats(7), cmarkfind-forget(7),
cmarkfind-exclude(7), cmarkfind-unexclude(7), cmarkfind-pin(7),
cmarkfind-unpin(7), cmarkfind-tag(7), cmarkfind-untag(7), cmarkfind-verbs(7),
cmarkfind-completion(7), cmarkfind-query(7),
cmarkfind-corpus(7), cmarkfind-exit-status(7)

markdownweb, cmarkbrowser, BonBon (Dave Winer)
