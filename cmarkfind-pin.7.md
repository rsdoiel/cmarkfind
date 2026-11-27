---
title: cmarkfind-pin
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-pin — keep a page near the top of the results

# SYNOPSIS

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
