---
title: cmarkfind-stats
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-stats — say what the corpus holds

# SYNOPSIS

cmarkfind stats [OPTIONS]

# DESCRIPTION

Say what the corpus holds: pages, pages gone, counts by site and tier, how many
listed pages have not been fetched yet, the file's size and its schema version,
and, when there are any, the very common words the index holds without
positions (see cmarkfind-indexer(7)); JSON has them as "document_only". A page
whose text is too big for one index file (about two million Chinese or Japanese
characters) is stored but left out of the index: a "not indexed" line names it
(JSON: "not_indexed", a list, empty when nothing is missing) until the page is
excluded, forgotten or replaced by a smaller one.

# OPTIONS

-json
: Write the result as JSON.

# EXIT STATUS

66 when there is no corpus yet. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
