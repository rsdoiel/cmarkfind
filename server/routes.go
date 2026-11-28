package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/cmarkfind/internal/render"
	"github.com/rsdoiel/markdownweb/fetch"
)

type format int

const (
	formatMarkdown format = iota
	formatHTML
	formatJSON
)

var formatTypes = map[format]string{
	formatMarkdown: "text/markdown; charset=utf-8",
	formatHTML:     "text/html; charset=utf-8",
	formatJSON:     "application/json",
}

var formatMedia = []struct {
	f    format
	name string
}{{formatMarkdown, "text/markdown"}, {formatHTML, "text/html"}, {formatJSON, "application/json"}}

var suffixes = map[string]format{".md": formatMarkdown, ".html": formatHTML, ".json": formatJSON}

// offered says which formats a page comes in.
type offered []format

func (o offered) has(f format) bool {
	for _, g := range o {
		if g == f {
			return true
		}
	}
	return false
}

var (
	pageFormats = offered{formatMarkdown, formatHTML}
	dataFormats = offered{formatMarkdown, formatHTML, formatJSON}
)

// route finds the page a path names and the format it is asked in (item 2, 3).
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/opensearch.xml" {
		s.description(w, r)
		return
	}
	base, suffix := p, ""
	for sfx := range suffixes {
		if strings.HasSuffix(p, sfx) {
			base, suffix = strings.TrimSuffix(p, sfx), sfx
		}
	}
	var offer offered
	var serve func(http.ResponseWriter, *http.Request, format)
	switch {
	case base == "/" && suffix == "":
		offer, serve = pageFormats, s.root
	case base == "/index" && suffix != "":
		offer, serve = pageFormats, s.root
	case base == "/search":
		offer, serve = dataFormats, s.search
	case base == "/version" && s.set.Info:
		offer, serve = dataFormats, s.version
	case base == "/stats" && s.set.Info:
		offer, serve = dataFormats, s.stats
	case base == "/sets" && s.set.Info:
		offer, serve = dataFormats, s.sets
	default:
		fail(w, http.StatusNotFound, "not found")
		return
	}
	accept := r.Header.Get("Accept")
	f, ok := formatOf(suffix, accept, offer)
	if suffix != "" && !offer.has(suffixes[suffix]) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if !ok {
		fail(w, http.StatusNotAcceptable, "not acceptable: this page is "+strings.Join(names(offer), ", "))
		return
	}
	serve(w, r, f)
}

func names(o offered) []string {
	var out []string
	for _, f := range o {
		for _, m := range formatMedia {
			if m.f == f {
				out = append(out, m.name)
			}
		}
	}
	return out
}

// mediaRange is one range of an Accept header.
type mediaRange struct {
	typ, sub string
	q        float64
}

// parseAccept reads an Accept header's ranges; a header with none that parse is
// the same as no header.
func parseAccept(accept string) []mediaRange {
	var out []mediaRange
	for _, part := range strings.Split(accept, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		t, sub, ok := strings.Cut(strings.ToLower(strings.TrimSpace(fields[0])), "/")
		if !ok || t == "" || sub == "" {
			continue
		}
		q := 1.0
		for _, param := range fields[1:] {
			if k, v, ok := strings.Cut(strings.TrimSpace(param), "="); ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		out = append(out, mediaRange{t, sub, q})
	}
	return out
}

// quality is the q the ranges give a media type: that of the most specific
// range that names it (type/subtype, then type/*, then */*); zero when none does.
func quality(ranges []mediaRange, name string) float64 {
	t, sub, _ := strings.Cut(name, "/")
	spec, q := -1, 0.0
	for _, rg := range ranges {
		s := -1
		switch {
		case rg.typ == t && rg.sub == sub:
			s = 2
		case rg.typ == t && rg.sub == "*":
			s = 1
		case rg.typ == "*" && rg.sub == "*":
			s = 0
		}
		if s > spec {
			spec, q = s, rg.q
		}
	}
	return q
}

// negotiate picks the offered format the Accept header likes best: each
// format takes the quality of the most specific range that names it, the
// highest wins, ties go to Markdown, then HTML, then JSON. No header, or */*,
// is Markdown.
func negotiate(accept string, offer offered) (format, bool) {
	ranges := parseAccept(accept)
	if len(ranges) == 0 {
		return formatMarkdown, offer.has(formatMarkdown)
	}
	best, bestQ, found := formatMarkdown, 0.0, false
	for _, m := range formatMedia {
		if !offer.has(m.f) {
			continue
		}
		if q := quality(ranges, m.name); q > 0 && q > bestQ {
			best, bestQ, found = m.f, q, true
		}
	}
	return best, found
}

// formatOf is the format of a request (item 3). A suffix names the format
// (.md, .html, .json), and that holds unless the client's Accept does not
// accept it at all: a client that asks only for Markdown, as BonBon does, gets
// Markdown at /search.html, and never a format it said it cannot take. Then the
// best format it does accept is served, and 406 when it accepts none the page
// offers. With no suffix the Accept header chooses, and no header is Markdown.
func formatOf(suffix, accept string, offer offered) (format, bool) {
	if suffix == "" {
		return negotiate(accept, offer)
	}
	f := suffixes[suffix]
	ranges := parseAccept(accept)
	if len(ranges) == 0 || quality(ranges, mediaOf(f)) > 0 {
		return f, true
	}
	return negotiate(accept, offer)
}

// mediaOf is the media type of a format, without parameters.
func mediaOf(f format) string {
	for _, m := range formatMedia {
		if m.f == f {
			return m.name
		}
	}
	return ""
}

// reply writes a 200 in a format.
func reply(w http.ResponseWriter, f format, body string) {
	w.Header().Set("Content-Type", formatTypes[f])
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(body))
}

func replyJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	reply(w, formatJSON, string(b)+"\n")
}

// firstValue is the first value of a query parameter; an empty one is absent.
func firstValue(q url.Values, name string) string {
	if v := q[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// search answers /search (item 4, 5).
func (s *Server) search(w http.ResponseWriter, r *http.Request, f format) {
	q := r.URL.Query()
	query := firstValue(q, "q")
	if query == "" {
		s.root(w, r, f) // a search with nothing to look for is the search page
		return
	}
	if len(query) > 1024 {
		fail(w, http.StatusBadRequest, "q: the query is over 1,024 bytes")
		return
	}
	count, start := s.set.Results, 1
	if v := firstValue(q, "count"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > s.set.Results {
			fail(w, http.StatusBadRequest, "count: a whole number from 1 to "+strconv.Itoa(s.set.Results))
			return
		}
		count = n
	}
	if v := firstValue(q, "startIndex"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, "startIndex: a whole number from 1")
			return
		}
		start = n
	}
	set := ""
	if v := firstValue(q, "set"); v != "" {
		key, err := s.db.ResolveSet(r.Context(), v)
		if err != nil {
			fail(w, http.StatusBadRequest, "set: no one set has that name")
			return
		}
		set = key
	}

	select { // at most Concurrency searches at once (item 12)
	case s.sem <- struct{}{}:
	default:
		s.busy.Add(1)
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusServiceUnavailable, "busy: try again")
		return
	}
	defer func() { <-s.sem }()
	s.flight.Add(1)
	defer s.flight.Add(-1)
	s.searches.Add(1)
	if s.BeforeSearch != nil {
		s.BeforeSearch()
	}

	res, err := s.db.SearchWith(r.Context(), query, corpus.SearchOptions{Limit: count, Offset: start - 1, Set: set, NoBuild: true})
	for _, warn := range s.db.IndexWarnings() { // the log, never the response
		s.logf("warning: %s", warn)
	}
	switch {
	case errors.Is(err, corpus.ErrAllIgnored):
		res, err = &corpus.Results{}, nil
	case errors.Is(err, corpus.ErrNeedsBuild):
		fail(w, http.StatusServiceUnavailable, "the index of that set needs building")
		return
	case errors.Is(err, corpus.ErrNoSet), errors.Is(err, corpus.ErrAmbiguousSet):
		fail(w, http.StatusBadRequest, "set: no one set has that name")
		return
	case errors.Is(err, fetch.ErrUnavailable):
		s.logf("a published index could not be reached: %v", err)
		fail(w, http.StatusBadGateway, "a published index could not be reached")
		return
	case index.IsDataError(err):
		s.logf("a published index is damaged: %v", err)
		fail(w, http.StatusInternalServerError, "a published index is damaged")
		return
	case err != nil:
		s.logf("internal error: %v", err)
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}

	pg := pageOf(query, set, start, count, res)
	switch f {
	case formatJSON:
		m := render.JSON(res, false)
		m["totalResults"], m["startIndex"], m["itemsPerPage"] = res.Total, start, count
		m["previous"], m["next"] = nilIfEmpty(pg.Prev), nilIfEmpty(pg.Next)
		replyJSON(w, m)
	case formatHTML:
		s.writeHTML(w, htmlResults(s.set.CSS, pg, res, set))
	default:
		reply(w, f, markdownResults(pg, res, set))
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
