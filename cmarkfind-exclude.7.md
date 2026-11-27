---
title: cmarkfind-exclude
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-exclude — keep an address prefix out of the corpus

# SYNOPSIS

cmarkfind exclude [OPTIONS] PREFIX

cmarkfind exclude -list

# DESCRIPTION

Keep every page whose address starts with PREFIX out of the corpus. Stored pages
under it are deleted; later fills skip them and do not request them.

PREFIX is an http:// or https:// address, matched as plain text, so end a folder
with a slash: https://example.org/drafts/ (without the slash,
https://example.org/drafts-old matches too).

# OPTIONS

-list
: Show the rules.

-json
: Write the result as JSON.

# EXAMPLES

    cmarkfind exclude https://example.org/drafts/
    cmarkfind exclude -list

# SEE ALSO

cmarkfind(1), cmarkfind-unexclude(7), cmarkfind-forget(7), cmarkfind-fill(7)
