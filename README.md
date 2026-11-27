
# cmarkfind

cmarkfind is a personal search engine for the Markdown web. It keeps the pages of the sites you read in one SQLite3 file and searches them locally with FTS5; nothing is sent anywhere by a search. It fills the file from a site's table of contents (`/.well-known/toc.opml`), from a folder on disk or over the web, using [markdownweb](https://github.com/rsdoiel/markdownweb). [cmarkbrowser](https://github.com/rsdoiel/cmarkbrowser) searches the same file.

~~~shell
cmarkfind fill https://rsdoiel.github.io
cmarkfind fill ~/Sites/example.org
cmarkfind search oberon compiler
cmarkfind stats
~~~

A big site can be read a few pages at a time with `fill -batch 25 -every 1h -until-done`. `exclude` keeps an address prefix out of the corpus; `forget` deletes a site's pages. `pin` puts a page first among the matches and `tag` gives it your own words; `search -pinned` and `search -tag` find them again. `cmarkfind -help` is the manual; `cmarkfind help index` lists the topics (one for each verb, and the query syntax, the corpus and the exit codes); `cmarkfind help TOPIC` shows one.

The corpus is `cmarkfind/corpus.db` in your user data directory, or the file named by `-db` or `CMARKFIND_DB`.

### Authors

- Doiel, R. S.

## Related resources

- [Getting Help, Reporting bugs](https://github.com/rsdoiel/cmarkfind/issues)
- [LICENSE](https://www.gnu.org/licenses/agpl-3.0.txt)
- [Installation](INSTALL.md)
- [Manual](cmarkfind.1.md)
- [Change log](CHANGES.md)
- [About](about.md)
