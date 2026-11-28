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
: The answer is no: search found nothing (also over an index); forget's SITE or unexclude's PREFIX
  had nothing to act on; robots.txt forbids the table of contents; completion
  -uninstall found nothing, or -check found a problem.

2
: Bad command line: unknown verb, option or topic, missing argument, bad value;
  a query is too large (over 1,024 bytes, 64 clauses or 8 levels of parentheses).

65
: The file is not a corpus or has a newer schema; a source is malformed; an
  index is damaged (a file does not match its digest, or the manifest is wrong).

66
: A folder, the corpus (for search and stats) or a table of contents is missing;
  an index is missing (no manifest.json in its folder).

69
: The site cannot be reached.

70
: A bug.

73
: The corpus file or its directory, or an index's directory, cannot be created;
  completion's script or its directory cannot be created, or a file cmarkfind did
  not write is in the way.

74
: A read or write failed part way: completion's startup file.

75
: The corpus is locked by another program; try again.

77
: The system refused access.

78
: The index folder (CMARKFIND_INDEX, or the folder beside the corpus) belongs to
  another corpus. Give each corpus a folder of its own.

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
