---
title: cmarkfind-indexer
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-indexer — build, check and list a search index

# SYNOPSIS

cmarkfind indexer build [-chunk N] [DIR]

cmarkfind indexer validate [DIR]

cmarkfind indexer [list] [DIR]

cmarkfind search -index DIR QUERY...

# DESCRIPTION

An index is a folder of compressed files, made from the corpus, that a program
can search without the corpus (and, later, over HTTP). build writes one from
every page the corpus holds that is not gone and not under an excluded prefix.
Building again replaces it: the generation number grows by one and files the
new manifest no longer names are removed once they are ten minutes old, so
a search that began before the rebuild can finish. The same pages always give the same
files.

validate reads every file and checks it against the digest in manifest.json.
list says what an index holds: pages, chunks, generation and when it was built,
and the words it holds without positions.

A word on a great many pages (a very common word such as "the", in any
language) can make its entry too big for one index file. Over about 2 MiB an
entry is written without positions: the index still knows which pages hold the
word and in which of title, description and body, and not where. A search for
the word, a prefix, a field clause and AND, OR and NOT work as before. A phrase
through such a word is checked against the text of each hit, so the answer is
exact; a query for a phrase through one reads the pages it shows, and the pages
above them that fail the check, and a limit keeps that small. The word is marked
in a snippet from the page's text. build
prints these words with their page and position counts, list prints the words, and a
fill, forget or exclude that rebuilds warns on standard error when the set of
words has changed. A word of more than 128 bytes (a hash, a pasted blob) is not
indexed at all. A build that still cannot fit a file names the word and exits
73; the index already in place is left as it was and keeps answering searches.

A word that is on more than a million pages is more than a reader will take (a
posting list is one array, and a reader refuses one of over a million elements),
so the build stops, names the word and its page count, and exits 73 instead of
writing an index every search would call damaged; the index already in place is
kept. A corpus of that size is far beyond what a rebuild after every fill suits.

A page whose own text is over one index file (about two million Chinese or
Japanese characters in a page) cannot be written. The build leaves that page out,
warns on standard error with its address, and goes on; the page stays in the
corpus and is not searchable. stats lists such pages as "not indexed".

DIR is optional. Without it the CMARKFIND_INDEX variable is used, else a folder
named index beside the corpus file (so -db moves both). The corpus builds its own index in that folder after every fill, forget or
exclude that changed pages, and plain search reads it; a search finding it
missing, or built by something else, builds it first. indexer build rebuilds it
by hand, and search -index DIR searches any other index.

The folder is the corpus's own: the first corpus to build into it writes a small
file, .cmarkfind-owner, naming itself, and a second corpus pointed at the same
folder is refused (exit 78) instead of rebuilding over the first on every
search. An index with no owner file (built by an older cmarkfind) goes to the
first corpus that builds it. indexer build DIR into any other folder does not
claim it.

# OPTIONS

-chunk N
: Entries per word file when building. Default 10000.

-json
: Write the result as JSON.

# EXIT STATUS

66 when DIR holds no index (or, for build, there is no corpus), 65 when an index
is damaged, 73 when DIR cannot be created. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind indexer build
    cmarkfind indexer validate
    cmarkfind search -index ~/.local/share/cmarkfind/index 'title:otter OR "shallow water"'

# SEE ALSO

cmarkfind(1), cmarkfind-search(7), cmarkfind-query(7), cmarkfind-corpus(7)
