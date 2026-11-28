---
title: cmarkfind-query
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-query — the search syntax

# DESCRIPTION

A query is a Lucene-style string, searched over each page's title, description
and body.

    oberon compiler          both words (AND is implied)
    oberon OR modula         either word
    oberon NOT pascal        the first and not the second (-pascal is the same)
    "shallow water"          the words next to each other, in order
    compil*                  words that start with compil
    title:oberon             the word in the title only (also description:, body:)
    (oberon OR modula) -tide groups

Case does not matter, nor does the spelling of an accent or a letter form: an
accent written as a combining mark, a ligature, a full-width letter and the
German sharp s match their plain forms (accents are not removed).

Chinese and Japanese are written without spaces, so each Han, Hiragana, Katakana
or Bopomofo character is a word of its own and a query of several is the phrase
of them: 東京都 finds the pages that have those three characters in a row, and
that includes 東京都 inside a longer run of text. There is no dictionary; a
search finds the characters you wrote, in order, and does not know where the
words of a sentence begin and end. Korean is written with spaces and its words
are indexed as written, with the particle attached (한국어를). A Korean word of
two or more syllables in a query is therefore read as a prefix: 한국어 finds
한국어를, 한국어는 and 한국어. A word in quotation marks is not (the phrase "한국어"
finds only the word 한국어), and a single syllable is not either. Thai, Lao,
Khmer and Burmese are not segmented: a run of letters between spaces is one
word, so a word in the middle of a clause cannot be found, only the start of
it with a prefix (ภาษา*).

A title match counts for more than a description match,
and a description match for more than one in the body (bm25 with weights 4, 2
and 1). Hits that rank the same are put in the order bookmarked, read, own, toc
pages, then by address.

Text that is not valid query syntax (an unbalanced quote, a stray operator) is
searched as plain words, so a typo returns hits and not an error; only a query
over the size limit is refused (exit 2). Words of another search engine's
syntax that this one does not have, such as NEAR(a b), ^word, a "phrase"~3 or
fuzzy word~, are read as the words they are made of. Pages marked gone are not
searched. The text shown with a hit is the 14 words of the page that hold the
most matches, each match in [brackets].

# EXAMPLES

    cmarkfind search '"shallow water" NOT tide'
    cmarkfind search -- -dash

# SEE ALSO

cmarkfind-search(7), cmarkfind-corpus(7)
