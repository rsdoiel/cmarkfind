---
title: cmarkfind-set
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-set — list and manage index sets

# SYNOPSIS

cmarkfind set list

cmarkfind set show SET

cmarkfind set ignore SET

cmarkfind set unignore SET

cmarkfind set fields [-reset] SET [FIELD=ROLE[,ROLE]...]...

# DESCRIPTION

An index set is one instance of a site: a table of contents on the web, or a
folder on this machine. The same site can be several sets (a copy you are
editing, the one on the local network, the public mirror), each searched on its
own. fill makes a set for each site it reads, and forget removes it with its
index. Each set has an index of its own (cmarkfind-indexer(7)).

search reads every set that is not ignored and merges them
(cmarkfind-search(7)); search-set reads one (cmarkfind-search-set(7)).

SET is the site as stats shows it, or its address with or without the scheme,
the trailing slash and /.well-known/toc.opml, so rsdoiel.github.io names
http://rsdoiel.github.io/.well-known/toc.opml; a folder is named by its path.
A name that fits more than one set (the http and https of one host) is
refused and the candidates are listed.

list names every set with its source, its pages and whether it is ignored.
show says what one holds, where its index is, its field mapping and, for a
collection (cmarkfind-fill(7)), the folder it reads. ignore
leaves a set out of search, so a near copy of a site you are not interested in
does not fill a lookup with duplicates; search-set still reaches it. unignore
puts it back. Nothing is ignored until you say so.

fields shows how a set reads the front matter of its pages (the YAML header of
a Markdown file): one line for each field with its roles. title, description
(with abstract as its alias) and body are built in. The default maps keywords
and author as facet,fielded, and date, datepublished, datemodified, pubdate and
created as date. Field names are lower case; a page's keys are matched without
regard to case. FIELD=ROLE,ROLE sets a field's roles, FIELD=none (or FIELD=)
turns a field off, even a default one, and -reset returns the set to the default.
The roles are facet (the field's whole values can be counted and followed),
fielded (field:word finds pages by the words of the field), date (the field is a
date for ranges and the year), and text (the field's words are searched with the
description). A page's front matter is kept when it is filled, so a page stored
before this was read again on the next fill. A date that cannot be read (not
YYYY, YYYY-MM, YYYY-MM-DD, or a day with a time) is warned about, once for the page
and field, and the page is left without it. A set's mapping changing makes its
index out of date, and the next build replaces it.

Pins, tags and exclusions belong to the corpus and apply in every set.

# OPTIONS

-reset
: With fields, return the set's field mapping to the default.

-json
: Write the result as JSON (list: {"sets": [...]}).

# EXIT STATUS

66 when SET is not a set, 2 when it fits more than one, an argument is missing, or
a role, a field name or an assignment is not usable (a built-in field cannot be
mapped).
list of a corpus with no sets is 0 and prints nothing. See
cmarkfind-exit-status(7).

# EXAMPLES

    cmarkfind set list
    cmarkfind set ignore tortuga.local
    cmarkfind set show rsdoiel.github.io
    cmarkfind set fields rsdoiel.github.io tags=facet,fielded keywords=none

# SEE ALSO

cmarkfind(1), cmarkfind-search(7), cmarkfind-search-set(7), cmarkfind-indexer(7)
