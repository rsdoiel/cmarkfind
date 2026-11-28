package server

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rsdoiel/cmarkfind/internal/render"
)

// ---- the OpenSearch description (item 6) ------------------------------------------------

type osdUrl struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
	Rel      string `xml:"rel,attr,omitempty"`
}

type osdDoc struct {
	XMLName     xml.Name `xml:"http://a9.com/-/spec/opensearch/1.1/ OpenSearchDescription"`
	ShortName   string
	Description string
	Input       string   `xml:"InputEncoding"`
	Output      string   `xml:"OutputEncoding"`
	Urls        []osdUrl `xml:"Url"`
}

// description answers /opensearch.xml: one document, for the Host asked for,
// that names no corpus.
func (s *Server) description(w http.ResponseWriter, r *http.Request) {
	base := "http://" + r.Host
	params := "?q={searchTerms}&count={count?}&startIndex={startIndex?}"
	doc := osdDoc{
		ShortName:   "cmarkfind",
		Description: "Search the Markdown pages in this cmarkfind corpus",
		Input:       "UTF-8",
		Output:      "UTF-8",
		Urls: []osdUrl{
			{Type: "text/markdown", Template: base + "/search" + params},
			{Type: "text/html", Template: base + "/search.html" + params},
			{Type: "application/json", Template: base + "/search.json" + params},
			{Type: "application/opensearchdescription+xml", Template: base + "/opensearch.xml", Rel: "self"},
		},
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		s.logf("internal error: %v", err)
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s\n%s\n", xml.Header[:len(xml.Header)-1], b)
}

// ---- /version -----------------------------------------------------------------------------------

func (s *Server) version(w http.ResponseWriter, r *http.Request, f format) {
	switch f {
	case formatJSON:
		replyJSON(w, map[string]any{"name": s.ver.Name, "version": s.ver.Version, "release_date": s.ver.ReleaseDate, "hash": s.ver.Hash})
	case formatHTML:
		pg := htmlPage{Title: "Version", CSS: s.set.CSS, Lines: []string{
			s.ver.Name + " " + s.ver.Version, "released " + s.ver.ReleaseDate, "hash " + s.ver.Hash}}
		s.writeHTML(w, pg)
	default:
		reply(w, f, fmt.Sprintf("# Version\n\n- name: %s\n- version: %s\n- release date: %s\n- hash: %s\n",
			render.Text(s.ver.Name), render.Text(s.ver.Version), render.Text(s.ver.ReleaseDate), render.Text(s.ver.Hash)))
	}
}

// ---- /sets -------------------------------------------------------------------------------------------

func (s *Server) sets(w http.ResponseWriter, r *http.Request, f format) {
	list, err := s.db.Sets(r.Context())
	if err != nil {
		s.logf("internal error: %v", err)
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch f {
	case formatJSON:
		rows := make([]map[string]any, 0, len(list))
		for _, x := range list {
			rows = append(rows, map[string]any{"name": x.Name, "source": x.Source, "pages": x.Pages, "ignored": x.Ignored, "built": x.Built})
		}
		replyJSON(w, map[string]any{"sets": rows})
	case formatHTML:
		pg := htmlPage{Title: "Sets", CSS: s.set.CSS, Head: []string{"name", "source", "pages", "ignored", "built"}}
		for _, x := range list {
			pg.Rows = append(pg.Rows, []string{render.Clean(x.Name), x.Source, strconv.Itoa(x.Pages), yesNo(x.Ignored), yesNo(x.Built)})
		}
		s.writeHTML(w, pg)
	default:
		var b strings.Builder
		b.WriteString("# Sets\n\n")
		for _, x := range list {
			fmt.Fprintf(&b, "- %s: %s, %d %s, ignored %s, built %s\n", render.Text(x.Name), x.Source, x.Pages, plural(x.Pages), yesNo(x.Ignored), yesNo(x.Built))
		}
		reply(w, f, b.String())
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func plural(n int) string {
	if n == 1 {
		return "page"
	}
	return "pages"
}

// ---- /stats ------------------------------------------------------------------------------------------

func (s *Server) settingValue(name string) any {
	switch name {
	case "port":
		return s.set.Port
	case "concurrency":
		return s.set.Concurrency
	case "results":
		return s.set.Results
	case "css":
		return s.set.CSS
	}
	return s.set.Info
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request, f format) {
	list, err := s.db.Sets(r.Context())
	if err != nil {
		s.logf("internal error: %v", err)
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	pages, by := 0, map[string]int{}
	for _, x := range list {
		pages += x.Pages
		by[x.Source]++
	}
	settings := map[string]any{}
	for _, n := range SettingNames {
		settings[n] = map[string]any{"value": s.settingValue(n), "stored": s.Stored[n]}
	}
	up := time.Since(s.started)
	m := map[string]any{
		"settings":       settings,
		"started":        s.started.UTC().Format(time.RFC3339),
		"uptime_seconds": int64(up.Seconds()),
		"requests":       map[string]any{"2xx": s.c2xx.Load(), "4xx": s.c4xx.Load(), "5xx": s.c5xx.Load()},
		"searches":       s.searches.Load(),
		"in_flight":      s.flight.Load(),
		"busy":           s.busy.Load(),
		"corpus":         map[string]any{"sets": len(list), "pages": pages, "local": by["local"], "mirror": by["mirror"], "reference": by["reference"]},
	}
	switch f {
	case formatJSON:
		replyJSON(w, m)
	case formatHTML:
		pg := htmlPage{Title: "Stats", CSS: s.set.CSS, Head: []string{"name", "value"}}
		for _, row := range statRows(s, m) {
			pg.Rows = append(pg.Rows, []string{row[0], row[1]})
		}
		s.writeHTML(w, pg)
	default:
		var b strings.Builder
		b.WriteString("# Stats\n\n")
		for _, row := range statRows(s, m) {
			fmt.Fprintf(&b, "- %s: %s\n", render.Text(row[0]), render.Text(row[1]))
		}
		reply(w, f, b.String())
	}
}

// statRows are the stats as name and value lines, for the Markdown and HTML pages.
func statRows(s *Server, m map[string]any) [][2]string {
	var rows [][2]string
	for _, n := range SettingNames {
		kind := "default"
		if s.Stored[n] {
			kind = "stored"
		}
		v := fmt.Sprint(s.settingValue(n))
		if n == "css" && v == "" {
			v = "(none)"
		}
		rows = append(rows, [2]string{n, v + " (" + kind + ")"})
	}
	rq := m["requests"].(map[string]any)
	c := m["corpus"].(map[string]any)
	rows = append(rows,
		[2]string{"started", fmt.Sprint(m["started"])},
		[2]string{"uptime seconds", fmt.Sprint(m["uptime_seconds"])},
		[2]string{"requests 2xx", fmt.Sprint(rq["2xx"])}, [2]string{"requests 4xx", fmt.Sprint(rq["4xx"])}, [2]string{"requests 5xx", fmt.Sprint(rq["5xx"])},
		[2]string{"searches", fmt.Sprint(m["searches"])}, [2]string{"in flight", fmt.Sprint(m["in_flight"])}, [2]string{"turned away busy", fmt.Sprint(m["busy"])},
		[2]string{"sets", fmt.Sprint(c["sets"])}, [2]string{"pages", fmt.Sprint(c["pages"])},
		[2]string{"local sets", fmt.Sprint(c["local"])}, [2]string{"mirror sets", fmt.Sprint(c["mirror"])}, [2]string{"reference sets", fmt.Sprint(c["reference"])})
	return rows
}
