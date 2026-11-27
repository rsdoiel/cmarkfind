
# Change log

## Unreleased

- Pin and tag: `pin` and `unpin` mark a page by its address so a pinned match is listed first; `tag` and `untag` give a page your own tags (letters, digits, `-`, `_`; folded to lower case; not part of the searched text). `pin -list` and `tag -list [ADDRESS]` show them. Pins and tags are kept by `forget` and by a refill.
- `search -tag TAG` (repeatable, a page must carry every tag) and `search -pinned`; with either, the query may be left out and the pages that qualify are listed by title. JSON hits gain `pinned` and `tags`; `stats` gains `pinned`, `tags` and `tag_pages`.
- Schema version 4 (two new tables); a version 3 file is upgraded in place. A cmarkbrowser built for schema 3 exits 65 on a version 4 file until it is released against this version: release cmarkfind first, then cmarkbrowser.
- Four new help topics: `pin`, `unpin`, `tag`, `untag`.
- Search reads the partitioned index (Lucene-style query language, BM25 with title 4, description 2, body 1) and not SQLite FTS5. `fill`, `forget` and `exclude` rebuild the index beside the corpus file (or in `CMARKFIND_INDEX`) when they changed pages; a search that finds it missing, or not the one this corpus built, builds it first (about 3 s for 340 pages, 6 s for 1,800). `indexer build` and `search -index DIR` stay.
- Schema version 5: the FTS5 table and its triggers are dropped on the first open (run `VACUUM` to give the space back). An older cmarkbrowser exits 65 on a version 5 file; move it to this cmarkfind first.
- Query differences from FTS5: `-word` and `title:word` are in; NEAR(), `^word`, column sets `{a b}:` and a prefix on a phrase are not (read as words). Snippets are the 14 words with the most matches, not FTS5's. Pins, tags and tier ties work as before.
- A query over the size limit exits 2; an index that cannot be written exits 73.

## v0.0.1, 2026-11-26

First release: a personal search engine for the Markdown web.

- The corpus (cmarkfind DR-0001): Markdown pages in one SQLite3 file, searched with FTS5 and ranked by bm25, ties broken by tier. Pure Go (`github.com/glebarez/go-sqlite`), no cgo. Schema version 3; older files are upgraded in place, a newer one is refused (exit 65).
- Verbs (cmarkfind DR-0002): `fill`, `search`, `stats`, `forget`, `exclude`, `unexclude`, `help`. `help index` lists nine topics (a page for each verb, and `query`, `corpus` and `exit-status`); each is a section 7 manual page, `cmarkfind-TOPIC.7.md`, written by `make build`. `-json` on every verb; exit codes follow the workspace convention and are documented under EXIT STATUS.
- `fill` reads a site's `/.well-known/toc.opml` from a folder or over the web. The web fill is polite: a pause per host, a named User-Agent, `robots.txt`, and conditional requests (`ETag`, `If-None-Match`) through markdownweb v0.0.5. A page the outline no longer lists is marked gone, not searched.
- Curation (cmarkfind DR-0003): `forget` a site or everything, `exclude` and `unexclude` an address prefix.
- Trickle (cmarkfind DR-0004): `fill -batch N` asks about at most N pages (never-asked first, then oldest checked) and says how many remain; `-every` and `-until-done` keep going.
- the next cmarkbrowser release searches and fills the same file (`S`, `F`, `-find`).
