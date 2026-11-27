---
title: cmarkfind-corpus
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-corpus — the file cmarkfind keeps

# DESCRIPTION

The corpus is one SQLite3 file. It holds, for each page, its address, site,
title, description, date, body, a hash of its content, the ETag and
Last-Modified the site sent, when it was fetched and last checked, its tier, and
when it was found gone. An FTS5 index over title, description and body does the
searching. Nothing leaves the machine when you search.

The file is named, in this order, by -db FILE, by the CMARKFIND_DB variable, or
it is cmarkfind/corpus.db in your user data directory. fill creates it; search
and stats need it to exist. Several programs may open it at once (WAL mode), and
cmarkbrowser searches and fills the same file.

The tier says how a page came in: own (a site on disk), toc (a table of contents
read over the web), read (a page you read in cmarkbrowser) or bookmarked. It
breaks ties between hits that rank the same.

The file carries a schema version. A file from an older cmarkfind is upgraded in
place; a file from a newer one is refused (exit 65), and the file is not
changed.

Page text comes from sites you do not control. Search results show it as text
only: no link, image, HTML or terminal hyperlink in it is made live.

# SEE ALSO

cmarkfind-fill(7), cmarkfind-search(7), cmarkfind-stats(7), cmarkfind-forget(7)
