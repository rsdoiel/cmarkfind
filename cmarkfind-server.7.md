---
title: cmarkfind-server
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-server — run a search service for this machine only

# SYNOPSIS

cmarkfind server [-json]

cmarkfind server port [-json] N

cmarkfind server set NAME VALUE

cmarkfind server show [-json]

# DESCRIPTION

server answers searches over the web, on this machine and no other, so a
browser, cmarkbrowser or a script can search the corpus. It listens on
127.0.0.1 only, answers only requests made for localhost or 127.0.0.1 with the
port it listens on, sends no CORS header, answers GET and HEAD only, and never
changes the corpus or builds an index. No login, no TLS: anyone who can use this
machine can search it. It prints listening on and its address as its first line
(with -json, {"url": ..., "port": ...}) and stops cleanly on Ctrl-C or SIGTERM.
A set whose index is missing is left out of searches, and server says how many
on standard error when it starts; run cmarkfind indexer build.

The addresses: / is a small search page; /search is the search; /opensearch.xml
describes the search for a browser, which can build a search box from it. The
description is OpenSearch 1.1: one Url for each of Markdown, HTML and JSON, whose
templates take {searchTerms} and the optional {count?} and {startIndex?}, so a
client uses the one for the format it reads. A browser finds it from the link in
the head of every HTML page. A client that reads Markdown or JSON has no head to
look in, so every response, errors included, also carries the header

    Link: </opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="cmarkfind"

with a relative target, which names the description at the address the client
already used. A program that asks for Markdown can read the header from any
answer (a HEAD request will do), fetch the description, and build a search from
the Markdown Url in it. The
search, and the pages below, come in three formats: Markdown (the default), HTML
(a .html suffix, or Accept: text/html) and JSON (.json, or Accept:
application/json). A suffix picks the format unless the client's Accept does not
accept it at all: a client that asks only for text/markdown, as BonBon does,
gets Markdown at /search.html, and a browser, whose Accept includes */*, gets
the HTML. The page /search.html has a form and a link a browser reads to find
the description.

/search takes q (the query, as cmarkfind-query(7) describes, at most 1,024
bytes), set (one set, by a name set list shows, else every set not ignored),
count (hits on a page, 1 to the results setting, which is the default too) and
startIndex (the first hit, counted from 1). A parameter that is not allowed is
400 and names itself. Every format says how many hits matched in all
(totalResults in JSON), where the page starts (startIndex) and how many it holds
(itemsPerPage), and links the page before and after (previous and next in JSON,
relative links such as ?q=oberon&startIndex=101&count=100). Markdown and HTML
say Hits 101 to 200 of 250. A facet is a link like any other: relative, so it
runs the narrowed search in the same format, at its first page.

Unless the info setting is off, /version says the program's name, version,
release date and hash; /stats says the settings (and which are stored), how long
the server has run, how many requests it answered by class, how many searches
ran and were turned away because it was busy, and how many sets and pages the
corpus has; and /sets lists the sets by the name set takes, with their source,
pages and whether they are ignored or built. A set's name is its site key, which
for a folder you filled is the folder's path: turn info off if other users of
this machine should not see it.

Searches are limited: concurrency of them at once (a further one is 503 with
Retry-After), a request line of 8 KiB, headers of 16 KiB, and time to send a
request. The HTML pages have no style of their own, so the browser's applies;
the css setting adds one stylesheet link.

server port N runs the service once on port N (0 takes a free port) and stores
nothing. server set NAME VALUE stores a setting in the corpus, to take effect
the next time the server starts, and default as VALUE removes it. server show
lists them. The settings are:

port
: The port, 0 to 65535, default 8900. A stored 0 takes a free port each time.

concurrency
: Searches at once, 1 to 256, default the number of CPUs (at least 2).

results
: The most hits on a page, 1 to 1000, default 100.

css
: A stylesheet to link from the HTML pages: an http or https address, or a path
  starting with a slash. The page's content security policy allows that one
  place for styles and nothing more. Default none.

info
: on or off: whether /version, /stats and /sets answer. Default on.

# OPTIONS

-json
: Write the result as JSON (for the first line, show and set).

# EXIT STATUS

2 for a bad sub-verb, setting name or value, or port; 66 when there is no
corpus; 73 when the port is in use or the listener cannot be made; 77 when the
system refuses the port (those below 1,024, for an ordinary user); 74 when a
setting cannot be written. A failed request is an HTTP status, never the exit
status. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind server show
    cmarkfind server set results 50
    cmarkfind server set css https://example.org/site.css
    cmarkfind server set info off
    cmarkfind server
    cmarkfind server port 0

# SEE ALSO

cmarkfind(1), cmarkfind-search(7), cmarkfind-query(7), cmarkfind-set(7), cmarkfind-indexer(7), cmarkfind-exit-status(7)
