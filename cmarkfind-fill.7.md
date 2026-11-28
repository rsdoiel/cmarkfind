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
