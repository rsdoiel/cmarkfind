
# Action Items

## Ideas to Explore

- [ ] cmarkfind should be able to leverage the cmarkbrowser's history and book files. 
- [ ] cmarkfind should be able to leverage knowledges bases configured in cmarkfind
- [ ] cmarkfind should use the verb, subverb DSL style we've established wth knowledge build's TUI providing both the scriptable actions but also a TUI to use it directly in a terminal.
- [ ] If I am exploring something using cmarkfind, I should be able to add it to a designated knowledge base.
- [ ] The server should give HTML results the HTML links. When a client asks for `text/html` (the `.html` suffix, or an `Accept` that chooses HTML), each hit should link to the equivalent `.html` page and not the `.md` address, so one cmarkfind service answers the Markdown Web and the HTML Web for the sites built with antennaApp (it writes both `x.md` and `x.html`). Markdown and JSON keep the stored address. Needs a short record first (the rule: BonBon's `.md`/`.html` and `index`/folder pairing, markdownweb's `toc.Mirror.Translate` is the opposite direction; a fixed rule or a setting; what `index.md` becomes; keep `#fragment`; leave `collection://` and non-`.md` addresses; whether JSON offers both; a missing twin is a broken link). See the Phase 8 hand-off.

- [x] The partitioned static index is the search engine (Phase 4 done 2026-11-27). Still to explore: a site publishing its own index for cmarkfind and cmarkbrowser to read (Phase 5 index sets).

## Modernization (2026-11-16)

To match the workspace standard (knowledge and opml are the models; Go 1.27.3 across repositories). Do one repository at a time; run the tests and `make -n release` after each step.

- [x] Makefile: add `generate`. Done 2026-11-26, from opml's Makefile.
- [x] Makefile: add `check-release` and make `release` run `generate` then `check-release`. Done 2026-11-26 (with `check-committed`); `make check-release` fails only on the uncommitted tree and the 0.0.0 release metadata.
- [x] Makefile: add `check-go-version`. Done 2026-11-26; codemeta.json now names `Go >= 1.27.4` and the markdownweb and go-sqlite requirements.
- [x] Drop the armv7l target and 32-bit Pi wording. Done 2026-11-26; the Makefile is opml's, with no armv7l target.
- [x] Add publish.bash and publish.ps1. Done 2026-11-26, from opml.
- [x] Add release.bash and release.ps1. Done 2026-11-26, from opml (the single-module script, not knowledge's nested-module one). Untried.
- [x] website.mak, page.tmpl, add-col-scope.lua, website.ps1: take knowledge's. Done 2026-11-26, from knowledge: PAGE_ORDER lists README, INSTALL, the two INSTALL_NOTES and about; toc.bash and css/site.css added; the dead User Manual nav link dropped (there is no user manual yet).

- [x] PROGRAMS = cmarkfind (2026-11-26, cmarkfind DR-0002 built). makefile_test.go and site_test.go added 2026-11-26 (from opml's; the website is built, with cmarkfind.1.md published from `make build`'s help output).
- [ ] CLI follow-ups (DR-0002 Open): all built 2026-11-27 (`fill -site-url`, `verbs`, `completion`). Completion of corpus values (pinned addresses, tags, sites) is not done.
- [x] Trickle: fill -batch/-every/-until-done built 2026-11-26 (cmarkfind DR-0004, proposed); schema version 3.
- [x] Curation: forget, exclude, unexclude built 2026-11-26 (cmarkfind DR-0003, proposed).
- [x] v0.0.1 release review 2026-11-26: go.mod on markdownweb v0.0.5, README/INSTALL/CHANGES.md brought current, codemeta.json 0.0.1. `help index` and nine per-topic section 7 pages added.
- [ ] Untried: `release.bash`, `publish.bash`, and the `.ps1` scripts; run `release.bash --dry-run` once the tree is committed.
