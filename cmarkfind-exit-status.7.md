---
title: cmarkfind-exit-status
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-exit-status — what the exit codes mean

# DESCRIPTION

cmarkfind uses the exit codes of the workspace convention. With -json a failure
is also written to standard output as {"error": "...", "class": "...", "code": N}.

# EXIT STATUS

0
: Success.

1
: The answer is no: search or search-set found nothing, or every set is ignored; forget's SITE or unexclude's PREFIX
  had nothing to act on; robots.txt forbids the table of contents; completion
  -uninstall found nothing, or -check found a problem; indexer discover found
  no published index; fill of a site whose set is a mirror or reference.

2
: Bad command line: unknown verb, option or topic, missing argument, bad value;
  a query is too large (over 1,024 bytes, 64 clauses or 8 levels of parentheses);
  a set name that fits more than one set (the http and https of one host).

65
: The file is not a corpus or has a newer schema; a source is malformed; an
  index is damaged (a file does not match its digest, or the manifest is wrong);
  a site's cmarkfind.json is wrong; an index to add or mirror fails verification;
  a published index has a lower generation than the one held (a rollback).

66
: A folder, the corpus (for search and stats) or a table of contents is missing;
  a set is not known; an index is missing (no manifest.json in its folder), or
  a site publishes none (indexer add).

69
: The site cannot be reached.

70
: A bug.

73
: The corpus file or its directory, or an index's directory, cannot be created;
  completion's script or its directory cannot be created, or a file cmarkfind did
  not write is in the way; indexer add of a name that is already a set; server on a port that is in use.

74
: A read or write failed part way: completion's startup file, or a server setting.

75
: The corpus is locked by another program; try again.

77
: The system refused access, or the port server was asked for.

78
: The index folder (CMARKFIND_INDEX, or the folder beside the corpus) belongs to
  another corpus. Give each corpus a folder of its own.

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
