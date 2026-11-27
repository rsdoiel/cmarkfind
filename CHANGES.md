
# Change log

## v0.0.1, 2026-11-26

First release: a personal search engine for the Markdown web.

- The corpus (cmarkfind DR-0001): Markdown pages in one SQLite3 file, searched with FTS5 and ranked by bm25, ties broken by tier. Pure Go (`github.com/glebarez/go-sqlite`), no cgo. Schema version 3; older files are upgraded in place, a newer one is refused (exit 65).
- Verbs (cmarkfind DR-0002): `fill`, `search`, `stats`, `forget`, `exclude`, `unexclude`, `help`. `help index` lists nine topics (a page for each verb, and `query`, `corpus` and `exit-status`); each is a section 7 manual page, `cmarkfind-TOPIC.7.md`, written by `make build`. `-json` on every verb; exit codes follow the workspace convention and are documented under EXIT STATUS.
- `fill` reads a site's `/.well-known/toc.opml` from a folder or over the web. The web fill is polite: a pause per host, a named User-Agent, `robots.txt`, and conditional requests (`ETag`, `If-None-Match`) through markdownweb v0.0.5. A page the outline no longer lists is marked gone, not searched.
- Curation (cmarkfind DR-0003): `forget` a site or everything, `exclude` and `unexclude` an address prefix.
- Trickle (cmarkfind DR-0004): `fill -batch N` asks about at most N pages (never-asked first, then oldest checked) and says how many remain; `-every` and `-until-done` keep going.
- the next cmarkbrowser release searches and fills the same file (`S`, `F`, `-find`).
