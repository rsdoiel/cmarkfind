---
title: cmarkfind-unpin
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-unpin — remove a pin

# SYNOPSIS

cmarkfind unpin ADDRESS...

# DESCRIPTION

Remove pins. The pages stay in the corpus.

# OPTIONS

-json
: Write {"unpinned": N}.

# EXIT STATUS

1 when none of the addresses was pinned. See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-pin(7)
