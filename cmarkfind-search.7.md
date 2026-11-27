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

Search the corpus and print each hit's title, address and tier, and the
matching text. The count and how many pages were searched go to standard error.
QUERY is words, "phrases", AND, OR, NOT and word*; see cmarkfind-query(7).
Text that is not valid query syntax is searched as plain words.

# OPTIONS

-limit N
: At most N hits. Default 10.

-index DIR
: Search the index in DIR (see cmarkfind-indexer(7)) instead of the corpus. The
  query is then the Lucene-style language of cmarkfind-query(7); -tag and
  -pinned do not apply. Exit 66 when DIR holds no index, 65 when it is damaged,
  2 when the query is too large.

-json
: Write {"hits": [...], "searched": N}. With -index each hit also has "excerpt"
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
