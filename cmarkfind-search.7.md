---
title: cmarkfind-search
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-search — search the corpus

# SYNOPSIS

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
words. The search reads the index of every set that is not ignored
(cmarkfind-set(7)), each set alone, and merges the answers by reciprocal rank
fusion, so a small site's best page is not buried by a large site's several
matches. A set's index is built after the fill that changed it
(cmarkfind-indexer(7)); one that is missing is built first, which takes a few
seconds on a large set. To search one set only, see cmarkfind-search-set(7).

# OPTIONS

-limit N
: At most N hits. Default 10.

-json
: Write {"hits": [...], "searched": N}; each hit has "anchor", the heading slug
  (empty when there is none).

Use -- before a query that starts with a dash.

# EXIT STATUS

1 when nothing matches, or every set is ignored; 66 when there is no corpus yet.
See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind search oberon compiler
    cmarkfind search -json '"shallow water"' | jq .

# SEE ALSO

cmarkfind(1), cmarkfind-query(7), cmarkfind-corpus(7)
