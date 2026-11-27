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
new manifest no longer names are removed. The same pages always give the same
files.

validate reads every file and checks it against the digest in manifest.json.
list says what an index holds: pages, chunks, generation and when it was built.

DIR is optional. Without it the CMARKFIND_INDEX variable is used, else a folder
named index beside the corpus file (so -db moves both). Plain search does not
read an index; only search -index does.

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
