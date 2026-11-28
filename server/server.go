// Package server is the loopback search service of cmarkfind (DR-0018): the
// handler that answers searches over a corpus, the OpenSearch description, and
// the settings the verb stores. It is a stub until Phase 8 is built.
package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/rsdoiel/cmarkfind/corpus"
)

var errNotBuilt = errors.New("server: not built")

// ErrBadSetting marks a setting name or value the server does not accept (exit 2).
var ErrBadSetting = errors.New("server: bad setting")

// Settings are the stored settings of the server (DR-0018 item 13).
type Settings struct {
	Port        int    // 0 is a free port
	Concurrency int    // searches at once
	Results     int    // most hits on a page
	CSS         string // a stylesheet to link; empty for none
	Info        bool   // the /version, /stats and /sets endpoints
}

// SettingNames is the settings, in the order show prints them.
var SettingNames = []string{"port", "concurrency", "results", "css", "info"}

// Version is what /version says.
type Version struct{ Name, Version, ReleaseDate, Hash string }

// Server is the handler and its counters.
type Server struct {
	// BeforeSearch, when set, is called after a search has been admitted and
	// before it runs (a test seam that holds searches open).
	BeforeSearch func()
	// ReadHeaderTimeout is how long a client has to send its headers; zero is 5 s.
	ReadHeaderTimeout time.Duration
	// Stored says which settings were stored and not defaulted, for /stats.
	Stored map[string]bool
	// Log is where warnings and internal errors go; nil is standard error.
	Log io.Writer
}

// ReadSettings reads the settings from db, with the defaults for any not stored,
// and says which names are stored.
func ReadSettings(ctx context.Context, db *corpus.DB) (Settings, map[string]bool, error) {
	return Settings{}, nil, errNotBuilt
}

// WriteSetting stores one setting; the value "default" removes it.
func WriteSetting(ctx context.Context, db *corpus.DB, name, value string) error { return errNotBuilt }

// New makes the server for db.
func New(db *corpus.DB, s Settings, v Version) *Server { return &Server{} }

// Handler is the http.Handler.
func (s *Server) Handler() http.Handler { return http.NotFoundHandler() }

// Listen binds 127.0.0.1 at port (0 for a free one).
func Listen(port int) (net.Listener, error) { return nil, errNotBuilt }

// Serve answers requests on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error { return errNotBuilt }
