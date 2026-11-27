---
title: cmarkfind-untag
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-untag — take tags off a page

# SYNOPSIS

cmarkfind untag ADDRESS TAG...

# DESCRIPTION

Take tags off a page.

# OPTIONS

-json
: Write {"address": ..., "removed": N}.

# EXIT STATUS

1 when the page carried none of the tags. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-tag(7)
