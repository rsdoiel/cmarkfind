---
title: cmarkfind-fill
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-fill — read a site's pages into the corpus

# SYNOPSIS

cmarkfind fill [OPTIONS] SOURCE

cmarkfind fill -name NAME [OPTIONS] FOLDER

# DESCRIPTION

Read the pages a site's table of contents (/.well-known/toc.opml) lists and
store them. SOURCE is a folder (a site on disk, nothing is fetched) or an
http:// or https:// address, either the site's address or its toc.opml.

Pages already stored are asked for again only if they changed: the web fill
sends If-None-Match and takes a 304 as unchanged. A page the table of contents
no longer lists is marked gone and is not searched. A page that is excluded (see
cmarkfind-exclude(7)) is neither stored nor requested.

A folder of Markdown files that is not a web site and has no outline (your notes,
a project's documents) is read with -name: every .md and .markdown file under
the folder becomes a page of the collection NAME, stored under
collection://NAME/path/to/file.md (the path is relative to the folder, with /
on every system, each part percent-encoded, so there is no drive letter and the
address does not change when the folder moves). A collection is an index set like
any other (cmarkfind-set(7)): search it with search-set NAME, or with the rest
with search. Files and folders whose names start with a dot, symbolic links and
files over 8 MiB are not read. The folder is remembered: fill -name NAME again
rereads it, and another folder under the same name is refused until forget NAME.
A relative link in a page resolves against its collection:// address.

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
: List at most N pages from the table of contents (with -name, read at most N files).

-depth N
: Follow included outlines N levels.

-site-url ADDRESS
: Folders only. Store every page under this published address (scheme, host and
  path prefix) whatever the outline lists, keeping each page's own path. Needs
  an http or https address with no query or fragment; exit 2 otherwise, or with a
  web site. Filling again with another address marks the old ones gone.

-name NAME
: Read FOLDER as the collection NAME (1 to 40 of a-z, 0-9, - and _, starting
  with a letter or digit), whatever outline it holds. Exit 2 with -site-url,
  -depth, -batch or a web address.

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
