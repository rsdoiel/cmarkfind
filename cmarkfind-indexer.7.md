---
title: cmarkfind-indexer
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-indexer — build, check and list a search index, and find a site's published one

# SYNOPSIS

cmarkfind indexer build [-chunk N] [SET]

cmarkfind indexer validate [SET]

cmarkfind indexer [list] [SET]

cmarkfind indexer discover SITE

# DESCRIPTION

An index is a folder of compressed files, made from the pages of one set
(cmarkfind-set(7)), that a program can search without the corpus (and, later,
over HTTP). Each set has an index folder of its own and there is no index of
the whole corpus. build writes the index of every set that needs one, or of
SET: a set needs one when it has none, when the one there is not the one this
corpus last built, or when what it is made from has changed (its pages after
exclusions, its field mapping). A set that needs nothing is left alone, and
build says how many it built and how many it left. Building again replaces an
index: the generation number grows by one and files the new manifest no longer
names are removed once they are ten minutes old, so a search that began before
the rebuild can finish. The same pages always give the same files, so a set's
folder is a complete index that can be published as it is.

validate reads every file of an index and checks it against the digest in
manifest.json. list says what an index holds: pages, chunks, generation and
when it was built, and the words it holds without positions.

discover asks where a site publishes its index and says what it found, and
changes nothing: no corpus is opened or made and no index file is fetched, only
the manifest. SITE is a site address (https://example.org/) or the address of a
manifest.json. For a site, discover reads /.well-known/cmarkfind.json at the
site's root, a small JSON object such as {"index": "/cmarkfind/manifest.json"}
whose "index" is a path or address on the same site, ending in manifest.json
(at most 64 KiB; a key may not repeat; other keys are ignored). When that file
is not there, discover tries /cmarkfind/manifest.json. A site that names a
manifest that is missing is not searched at the default path instead. It
prints the manifest's address, how it was found, and the generation, page
count and build time the manifest declares.

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

The indexes are kept in the folder named by the CMARKFIND_INDEX variable, else
a folder named index beside the corpus file (so -db moves both), one folder
under it for each set. A fill, forget or exclude that changed pages builds the
indexes of the sets it touched, and plain search reads them; a search that finds
one missing, or built by something else, builds it first. forget removes the
set's index with its pages. An older cmarkfind kept one index for the whole
corpus at the top of that folder; the first build removes it.

The folder is the corpus's own: the first corpus to build into it writes a small
file, .cmarkfind-owner, naming itself, and a second corpus pointed at the same
folder is refused (exit 78) instead of rebuilding over the first on every
search. A folder with no owner file goes to the first corpus that builds in it.

# OPTIONS

-chunk N
: Entries per word file when building. Default 10000.

-json
: Write the result as JSON.

# EXIT STATUS

66 when SET is not a set or has no index (or there is no corpus), 2 when SET
fits more than one set, 65 when an index is damaged, 73 when an index cannot be
written. validate checks every set and exits with the first failure's class.
discover exits 1 when the site publishes no index (or its cmarkfind.json names
a manifest that is missing), 2 for a missing or bad address, 65 when its
cmarkfind.json or manifest is wrong (another origin, not a manifest.json, a
repeated key, over 64 KiB, a format or analyzer this program does not know),
and 69 when the site cannot be reached. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind indexer build
    cmarkfind indexer build rsdoiel.github.io
    cmarkfind indexer validate
    cmarkfind indexer discover https://example.org/

# SEE ALSO

cmarkfind(1), cmarkfind-set(7), cmarkfind-search(7), cmarkfind-query(7), cmarkfind-corpus(7)
