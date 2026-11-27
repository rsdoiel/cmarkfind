---
title: cmarkfind-query
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-query — the search syntax

# DESCRIPTION

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
