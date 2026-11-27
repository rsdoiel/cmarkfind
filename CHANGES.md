
# Change log

## Unreleased

- Pin and tag: `pin` and `unpin` mark a page by its address so a pinned match is listed first; `tag` and `untag` give a page your own tags (letters, digits, `-`, `_`; folded to lower case; not part of the searched text). `pin -list` and `tag -list [ADDRESS]` show them. Pins and tags are kept by `forget` and by a refill.
- `search -tag TAG` (repeatable, a page must carry every tag) and `search -pinned`; with either, the query may be left out and the pages that qualify are listed by title. JSON hits gain `pinned` and `tags`; `stats` gains `pinned`, `tags` and `tag_pages`.
- Schema version 4 (two new tables); a version 3 file is upgraded in place. A cmarkbrowser built for schema 3 exits 65 on a version 4 file until it is released against this version: release cmarkfind first, then cmarkbrowser.
- Four new help topics: `pin`, `unpin`, `tag`, `untag`.

## v0.0.1, 2026-11-26

First release: a personal search engine for the Markdown web.

- The corpus (cmarkfind DR-0001): Markdown pages in one SQLite3 file, searched with FTS5 and ranked by bm25, ties broken by tier. Pure Go (`github.com/glebarez/go-sqlite`), no cgo. Schema version 3; older files are upgraded in place, a newer one is refused (exit 65).
- Verbs (cmarkfind DR-0002): `fill`, `search`, `stats`, `forget`, `exclude`, `unexclude`, `help`. `help index` lists nine topics (a page for each verb, and `query`, `corpus` and `exit-status`); each is a section 7 manual page, `cmarkfind-TOPIC.7.md`, written by `make build`. `-json` on every verb; exit codes follow the workspace convention and are documented under EXIT STATUS.
- `fill` reads a site's `/.well-known/toc.opml` from a folder or over the web. The web fill is polite: a pause per host, a named User-Agent, `robots.txt`, and conditional requests (`ETag`, `If-None-Match`) through markdownweb v0.0.5. A page the outline no longer lists is marked gone, not searched.
- Curation (cmarkfind DR-0003): `forget` a site or everything, `exclude` and `unexclude` an address prefix.
- Trickle (cmarkfind DR-0004): `fill -batch N` asks about at most N pages (never-asked first, then oldest checked) and says how many remain; `-every` and `-until-done` keep going.
- the next cmarkbrowser release searches and fills the same file (`S`, `F`, `-find`).
