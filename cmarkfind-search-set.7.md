---
title: cmarkfind-search-set
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-search-set — search one set

# SYNOPSIS

cmarkfind search-set [OPTIONS] SET QUERY...

# DESCRIPTION

Search one index set (cmarkfind-set(7)) and print the hits as search does
(cmarkfind-search(7)). The set is searched whether or not it is ignored, and
nothing from any other set is shown. SET is named as for set show.

# OPTIONS

-limit N
: At most N hits. Default 10.

-tag TAG
: Only pages with the tag; repeat for several.

-pinned
: Only pinned pages.

-json
: As search.

# EXIT STATUS

1 when nothing matches, 66 when SET is not a set, 2 when it fits more than one.
See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind search-set rsdoiel.github.io "OBNC AND Oberon Compiler"

# SEE ALSO

cmarkfind(1), cmarkfind-search(7), cmarkfind-set(7), cmarkfind-query(7)
