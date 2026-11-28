// Package server is the loopback search service of cmarkfind (DR-0018): the
// handler that answers searches over a corpus, the OpenSearch description, and
// the settings the verb stores. It binds 127.0.0.1 only, answers only requests
// made for the address it listens on, is read-only, and never builds an index.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rsdoiel/cmarkfind/corpus"
)

// ErrBadSetting marks a setting name or value the server does not accept (exit 2).
var ErrBadSetting = errors.New("server: bad setting")

/** Settings are the stored settings of the server (DR-0018 item 13).
 *
 * Fields:
 *   Port (int)        — the port; 0 is a free one
 *   Concurrency (int) — searches that may run at once
 *   Results (int)     — the most hits on a page, and the page size when none is asked for
 *   CSS (string)      — a stylesheet to link from the HTML pages; empty for none
 *   Info (bool)       — whether /version, /stats and /sets are answered
 *   Title (string)    — the service's name, in the description and wherever it names itself; empty is "cmarkfind"
 */
type Settings struct {
	Port        int
	Concurrency int
	Results     int
	CSS         string
	Info        bool
	Title       string
}

// SettingNames is the settings, in the order show prints them.
var SettingNames = []string{"port", "concurrency", "results", "css", "info", "title"}

/** Version is what /version says.
 *
 * Fields:
 *   Name, Version, ReleaseDate, Hash (string) — the program's own, from version.go
 */
type Version struct{ Name, Version, ReleaseDate, Hash string }

/** Server is the handler of the service and its counters. Make one with New.
 *
 * Fields:
 *   BeforeSearch (func())          — a test seam, called after a search was admitted and before it runs
 *   ReadHeaderTimeout (time.Duration) — how long a client has to send its headers; zero is 5 s
 *   Stored (map[string]bool)       — which settings were stored and not defaulted, for /stats
 *   Log (io.Writer)                — where warnings and internal errors go; nil is standard error
 */
type Server struct {
	BeforeSearch      func()
	ReadHeaderTimeout time.Duration
	Stored            map[string]bool
	Log               io.Writer

	db   *corpus.DB
	set  Settings
	ver  Version
	link string // the Link header of every response (item 6)

	port    atomic.Int64
	started time.Time
	sem     chan struct{}

	logMu                  sync.Mutex
	c2xx, c4xx, c5xx       atomic.Int64
	searches, busy, flight atomic.Int64
}

/** New makes the server for db with the given settings.
 *
 * Parameters:
 *   db (*corpus.DB)  — the corpus; it is only read
 *   s (Settings)     — the settings in force
 *   v (Version)      — what /version says
 *
 * Returns:
 *   *Server — the handler; give it a listener with Serve
 *
 * Example:
 *   srv := server.New(db, server.Defaults(), server.Version{Name: "cmarkfind", Version: "0.0.1"})
 */
func New(db *corpus.DB, s Settings, v Version) *Server {
	if s.Concurrency < 1 {
		s.Concurrency = 1
	}
	if s.Results < 1 {
		s.Results = 100
	}
	if s.Title == "" {
		s.Title = DefaultTitle
	}
	return &Server{db: db, set: s, ver: v, started: time.Now(), sem: make(chan struct{}, s.Concurrency),
		link: `</opensearch.xml>; rel="search"; type="application/opensearchdescription+xml"; title="` + s.Title + `"`}
}

/** Listen binds 127.0.0.1 at port, and only that address.
 *
 * Parameters:
 *   port (int) — 0 to 65535; 0 takes a free port
 *
 * Returns:
 *   net.Listener — the listener
 *   error        — the system's (an address in use, a port refused)
 *
 * Example:
 *   ln, err := server.Listen(0)
 */
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

/** Serve answers requests on ln until ctx is done, then stops, giving requests
 * in progress a few seconds to finish.
 *
 * Parameters:
 *   ctx (context.Context) — cancelled to stop the server
 *   ln (net.Listener)     — from Listen
 *
 * Returns:
 *   error — nil after a clean stop; the listener's error otherwise
 *
 * Example:
 *   err := srv.Serve(ctx, ln)
 */
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		s.port.Store(int64(tcp.Port))
	}
	rht := s.ReadHeaderTimeout
	if rht == 0 {
		rht = 5 * time.Second
	}
	hs := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: rht,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	go func() { done <- hs.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(shut)
	if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// logf writes one line to the log.
func (s *Server) logf(format string, a ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	w := s.Log
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "cmarkfind: "+format+"\n", a...)
}

// statusRecorder notes the status a handler wrote, for the counters.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Handler is the http.Handler: the checks every request passes, then the routes.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				s.logf("internal error: %v", p)
				if rec.status == 0 {
					fail(rec, http.StatusInternalServerError, "internal error")
				}
			}
			switch {
			case rec.status >= 500:
				s.c5xx.Add(1)
			case rec.status >= 400:
				s.c4xx.Add(1)
			case rec.status != 0:
				s.c2xx.Add(1)
			}
		}()
		s.serve(rec, r)
	})
}

// DefaultTitle is the service's name when the title setting is not stored.
const DefaultTitle = "cmarkfind"

// setHeaders are on every response, errors included (item 9). The Link header
// names the OpenSearch description, so a client that asks for Markdown, which has
// no head to carry a <link>, still learns that search is here and where its
// description is (item 6). The target is relative: it is the description at the
// address the client already reached; its title is the title setting.
func (s *Server) setHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Link", s.link)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Vary", "Accept")
}

// fail answers with a status and one fixed line of plain text.
func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintln(w, msg)
}

// hostAllowed is true for localhost and 127.0.0.1 with the port the listener has.
func (s *Server) hostAllowed(host string) bool {
	port := strconv.FormatInt(s.port.Load(), 10)
	host = strings.ToLower(host)
	return host == "localhost:"+port || host == "127.0.0.1:"+port
}

// serve is the checks of items 9 and 10, then the routes.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.setHeaders(w)
	if !s.hostAllowed(r.Host) {
		fail(w, http.StatusForbidden, "forbidden: not a request for this address")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "none" && site != "same-origin" {
		fail(w, http.StatusForbidden, "forbidden: a request made by another page")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(r.RequestURI) > 8<<10 {
		fail(w, http.StatusRequestURITooLong, "request address too long")
		return
	}
	s.route(w, r)
}
