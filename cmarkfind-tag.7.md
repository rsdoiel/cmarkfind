---
title: cmarkfind-tag
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-tag — give a page your own tags

# SYNOPSIS

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
