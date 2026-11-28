---
title: cmarkfind-forget
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-forget — delete a site's pages, or every page

# SYNOPSIS

cmarkfind forget [OPTIONS] SITE

cmarkfind forget -all

# DESCRIPTION

Delete the pages stored from one site (a folder or an address, named as for
cmarkfind-fill(7); a collection by its name), or every page. The site's set (cmarkfind-set(7)) and its
index go with its pages. Exclusions are kept, so a later fill does not bring back
what exclude keeps out.

# OPTIONS

-all
: Forget every page.

-dry-run
: Say how many pages would go; delete nothing.

-json
: Write {"forgotten": N, "dry_run": false}.

# EXIT STATUS

1 when SITE stored nothing. See cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind forget -dry-run https://example.org
    cmarkfind forget ~/Sites/example.org

# SEE ALSO

cmarkfind(1), cmarkfind-exclude(7), cmarkfind-fill(7)
