---
title: cmarkfind
abstract: |-
  cmarkfind is a personal search engine for the Markdown web. It keeps pages read from a site's table of contents (toc.opml), on disk or over the web, in one SQLite3 file and searches them locally with a partitioned index (BM25 ranking, phrases, Unicode and CJK aware) built beside it. cmarkbrowser searches the same corpus.
authors:
  - family_name: Doiel
    given_name: R. S.
    id: https://orcid.org/0000-0003-0900-6903



repository_code: https://github.com/rsdoiel/cmarkfind
version: 0.0.1
license_url: https://www.gnu.org/licenses/agpl-3.0.txt

programming_language:
  - Go

keywords:
  - personal search engine
  - Markdown
  - CommonMark
  - front matter
  - Markdown Web

date_released: 2026-11-26
---

About this software
===================

## cmarkfind 0.0.1

- first release: a corpus of Markdown pages in one SQLite3 FTS5 file (pure Go, no cgo), searched locally with bm25 ranking
- fill reads a site's toc.opml from a folder or over the web, politely (pause per host, robots.txt, conditional requests), in resumable batches (-batch, -every, -until-done)
- verbs: fill, search, stats, forget, exclude, unexclude, help (help index lists nine topics, each a section 7 man page); -json everywhere; exit codes per the workspace convention
- the next cmarkbrowser release searches and fills the same corpus (S, F, -find)

## Authors

- [R. S. Doiel](https://orcid.org/0000-0003-0900-6903)






cmarkfind is a personal search engine for the Markdown web. It keeps pages read from a site's table of contents (toc.opml), on disk or over the web, in one SQLite3 file and searches them locally with a partitioned index (BM25 ranking, phrases, Unicode and CJK aware) built beside it. cmarkbrowser searches the same corpus.

- [License](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Code Repository](https://github.com/rsdoiel/cmarkfind)
  - [Issue Tracker](https://github.com/rsdoiel/cmarkfind/issues)

## Programming languages

- Go




## Software Requirements

- Go >= 1.27.4
- github.com/rsdoiel/markdownweb >= 0.0.5
- github.com/glebarez/go-sqlite >= 1.23.0


## Software Suggestions

- CMTools >= 0.0.45b
- Pandoc >= 3.9
- GNU Make >= 3


