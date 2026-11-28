---
title: cmarkfind-verbs
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-verbs — list the verbs and their options

# SYNOPSIS

cmarkfind verbs [-json]

# DESCRIPTION

List every verb with a one-line summary. With -json the output is
{"verbs": [{"name", "summary", "subverbs", "flags"}]}: the flags of each verb,
each with its name and whether it is a switch ("bool": true) or takes a value.
A verb with subverbs (indexer) lists the flags of each under "sub". The lists
are read from the verbs themselves, the ones shell completion uses. It opens no
corpus.

# OPTIONS

-json
: Write the list as JSON.

# EXIT STATUS

0
: Success.

2
: An argument or an unknown option.

See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-completion(7)
