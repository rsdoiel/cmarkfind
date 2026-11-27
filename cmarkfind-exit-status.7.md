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
: The answer is no: search found nothing; forget's SITE or unexclude's PREFIX
  had nothing to act on; robots.txt forbids the table of contents.

2
: Bad command line: unknown verb, option or topic, missing argument, bad value.

65
: The file is not a corpus or has a newer schema; a source is malformed.

66
: A folder, the corpus (for search and stats) or a table of contents is missing.

69
: The site cannot be reached.

70
: A bug.

73
: The corpus file or its directory cannot be created.

75
: The corpus is locked by another program; try again.

77
: The system refused access.

# SEE ALSO

cmarkfind(1), cmarkfind-corpus(7)
