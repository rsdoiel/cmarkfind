---
title: cmarkfind-unexclude
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-unexclude — remove an exclusion

# SYNOPSIS

cmarkfind unexclude PREFIX

# DESCRIPTION

Remove an exclusion. The pages it kept out come back with the next fill.

# OPTIONS

-json
: Write {"prefix": ..., "removed": true|false}.

# EXIT STATUS

1 when there was no such rule. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-exclude(7)
