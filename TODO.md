
# Action Items

## Modernization (2026-11-16)

To match the workspace standard (knowledge and opml are the models; Go 1.27.3 across repositories). Do one repository at a time; run the tests and `make -n release` after each step.

- [x] Makefile: add `generate`. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] Makefile: add `check-release` and make `release` run `generate` then `check-release`. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] Makefile: add `check-go-version`. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] Drop the armv7l target and 32-bit Pi wording. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] Add publish.bash and publish.ps1. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] Add release.bash and release.ps1. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
- [x] website.mak, page.tmpl, add-col-scope.lua, website.ps1: take knowledge's. Not applicable yet: cmarkfind holds only notes and a template Makefile; there is no Go code or go.mod yet. Redo the scaffolding from opml when code starts.
