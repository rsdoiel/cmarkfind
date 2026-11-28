package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Phase 8 (cmarkfind DR-0018 item 13): the server verb and its sub-verbs. The
// service itself is tested in package server; here are the settings, the start
// line, the exit codes and the clean stop.

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// the first line is `listening on URL`, or with -json {"url":"URL","port":N}
var listening = regexp.MustCompile(`(?:listening on |"url":")(http://127\.0\.0\.1:(\d+)/)`)

// serving is a running `cmarkfind server`: its address, and how to stop it.
type serving struct {
	url    string
	port   int
	stdout *syncBuf
	stderr *syncBuf
	stop   func() int
}

// runServer starts the verb with args under a context, waits for its first line
// and returns. The server is stopped when the test ends if the test has not.
func runServer(t *testing.T, args ...string) *serving {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	o, e := &syncBuf{}, &syncBuf{}
	code := make(chan int, 1)
	go func() { code <- RunContext(ctx, args, o, e) }()
	s := &serving{stdout: o, stderr: e}
	var once sync.Once
	var exit int
	s.stop = func() int {
		once.Do(func() {
			cancel()
			select {
			case exit = <-code:
			case <-time.After(10 * time.Second):
				t.Error("the server did not stop when cancelled")
				exit = -1
			}
		})
		return exit
	}
	t.Cleanup(func() { s.stop() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := listening.FindStringSubmatch(o.String()); m != nil {
			s.url = m[1]
			s.port, _ = strconv.Atoi(m[2])
			return s
		}
		select {
		case c := <-code: // it ended before it listened
			code <- c
			t.Fatalf("server %v ended with %d before listening\nstdout: %s\nstderr: %s", args, c, o, e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server %v printed no listening line\nstdout: %q\nstderr: %q", args, o.String(), e.String())
	return nil
}

func (s *serving) get(t *testing.T, path string) (int, string) {
	t.Helper()
	r, err := http.Get(s.url + strings.TrimPrefix(path, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ---- show and set -------------------------------------------------------------------------

func TestServerShowListsEveryFiveSettingsWithTheirDefaults(t *testing.T) {
	db := filled(t)
	r := do(t, "-db", db, "server", "show")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"port 8900 (default)", "results 100 (default)", "info on (default)", "css  (default)", "concurrency", "title cmarkfind (default)"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("show lacks %q:\n%s", want, r.out)
		}
	}
	j := do(t, "-db", db, "server", "show", "-json")
	m := parseJSON(t, j.out)["settings"].(map[string]any)
	for _, name := range []string{"port", "concurrency", "results", "css", "info", "title"} {
		s, ok := m[name].(map[string]any)
		if !ok || s["stored"] != false {
			t.Errorf("setting %s in JSON: %v", name, m[name])
		}
	}
	if m["port"].(map[string]any)["value"] != float64(8900) || m["results"].(map[string]any)["value"] != float64(100) || m["info"].(map[string]any)["value"] != true {
		t.Errorf("default values: %v", m)
	}
}

func TestServerSetStoresASettingAndShowReadsItBackAndDefaultRemovesIt(t *testing.T) {
	db := filled(t)
	for _, c := range [][2]string{{"port", "9123"}, {"concurrency", "5"}, {"results", "25"}, {"css", "https://x.example/a.css"}, {"info", "off"}, {"title", "wren notes"}} {
		if r := do(t, "-db", db, "server", "set", c[0], c[1]); r.code != 0 {
			t.Fatalf("set %s %s: %+v", c[0], c[1], r)
		}
	}
	show := do(t, "-db", db, "server", "show").out
	for _, want := range []string{"port 9123 (stored)", "concurrency 5 (stored)", "results 25 (stored)", "css https://x.example/a.css (stored)", "info off (stored)", "title wren notes (stored)"} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	if r := do(t, "-db", db, "server", "set", "results", "default"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if show := do(t, "-db", db, "server", "show").out; !strings.Contains(show, "results 100 (default)") || !strings.Contains(show, "port 9123 (stored)") {
		t.Errorf("after default:\n%s", show)
	}
	// a stored setting survives into the next process: the corpus holds it
	if r := do(t, "-db", db, "server", "set", "port", "0"); r.code != 0 || !strings.Contains(do(t, "-db", db, "server", "show").out, "port 0 (stored)") {
		t.Errorf("a stored port 0: %+v", r)
	}
}

func TestServerSetAndPortExitCodes(t *testing.T) {
	db := filled(t)
	missing := dbPath(t)
	for name, tc := range map[string]struct {
		args []string
		want int
	}{
		"no setting name":       {[]string{"server", "set"}, 2},
		"no value":              {[]string{"server", "set", "port"}, 2},
		"too many arguments":    {[]string{"server", "set", "port", "9000", "x"}, 2},
		"an unknown setting":    {[]string{"server", "set", "colour", "red"}, 2},
		"a bad port":            {[]string{"server", "set", "port", "99999"}, 2},
		"a bad concurrency":     {[]string{"server", "set", "concurrency", "0"}, 2},
		"a bad results":         {[]string{"server", "set", "results", "5000"}, 2},
		"a bad css":             {[]string{"server", "set", "css", "ftp://x/a.css"}, 2},
		"a bad info":            {[]string{"server", "set", "info", "maybe"}, 2},
		"a title too long":      {[]string{"server", "set", "title", "this title is far too long"}, 2},
		"a title with a quote":  {[]string{"server", "set", "title", `say "hi"`}, 2},
		"show with an argument": {[]string{"server", "show", "port"}, 2},
		"an unknown subverb":    {[]string{"server", "start"}, 2},
		"port with no number":   {[]string{"server", "port"}, 2},
		"port with a word":      {[]string{"server", "port", "abc"}, 2},
		"port negative":         {[]string{"server", "port", "-1"}, 2},
		"port out of range":     {[]string{"server", "port", "65536"}, 2},
		"port with two numbers": {[]string{"server", "port", "9000", "9001"}, 2},
		"an unknown option":     {[]string{"server", "-nope"}, 2},
	} {
		if r := do(t, append([]string{"-db", db}, tc.args...)...); r.code != tc.want {
			t.Errorf("%s: exit %d, want %d: %+v", name, r.code, tc.want, r)
		}
	}
	for name, args := range map[string][]string{
		"show with no corpus": {"server", "show"}, "set with no corpus": {"server", "set", "port", "9000"}, "port with no corpus": {"server", "port", "0"}, "run with no corpus": {"server"},
	} {
		if r := do(t, append([]string{"-db", missing}, args...)...); r.code != 66 {
			t.Errorf("%s: exit %d, want 66: %+v", name, r.code, r)
		}
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a server verb made a corpus")
	}
	// nothing bad was stored
	if show := do(t, "-db", db, "server", "show").out; strings.Contains(show, "(stored)") {
		t.Errorf("a refused setting was stored:\n%s", show)
	}
}

// ---- running ---------------------------------------------------------------------------------------

func TestServerPortRunsOnceOnAFreePortPrintsItsAddressAndStopsCleanly(t *testing.T) {
	db := filled(t)
	s := runServer(t, "-db", db, "server", "port", "0")
	if !strings.HasPrefix(s.stdout.String(), "listening on http://127.0.0.1:") || s.port == 0 {
		t.Fatalf("first line: %q", s.stdout.String())
	}
	if code, body := s.get(t, "/search.json?q=otter"); code != 200 || !strings.Contains(body, "https://example.org/a.md") {
		t.Errorf("search over the verb's corpus: %d\n%s", code, body)
	}
	if code, _ := s.get(t, "/version"); code != 200 {
		t.Errorf("/version: %d", code)
	}
	if got := s.stop(); got != 0 {
		t.Errorf("a clean stop exited %d, want 0", got)
	}
	if _, err := http.Get(s.url); err == nil {
		t.Error("the server still answers after it stopped")
	}
}

func TestServerJSONPrintsTheAddressAsAnObject(t *testing.T) {
	db := filled(t)
	s := runServer(t, "-db", db, "server", "port", "-json", "0")
	line := strings.SplitN(s.stdout.String(), "\n", 2)[0]
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil || m["url"] != s.url || m["port"] != float64(s.port) {
		t.Errorf("first line %q: %v", line, err)
	}
}

func TestAStoredPortIsUsedAndServerPortOverridesItForOneRunOnly(t *testing.T) {
	db := filled(t)
	n := freePort(t)
	if r := do(t, "-db", db, "server", "set", "port", fmt.Sprint(n)); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	s := runServer(t, "-db", db, "server")
	if s.port != n {
		t.Errorf("the stored port %d was not used: %d", n, s.port)
	}
	s.stop()
	o := runServer(t, "-db", db, "server", "port", "0")
	if o.port == n || o.port == 0 {
		t.Errorf("server port 0 took %d, want some other free port", o.port)
	}
	o.stop()
	if show := do(t, "-db", db, "server", "show").out; !strings.Contains(show, fmt.Sprintf("port %d (stored)", n)) {
		t.Errorf("server port changed the stored port:\n%s", show)
	}
}

func TestStoredSettingsTakeEffectOnTheNextRun(t *testing.T) {
	db := filled(t)
	do(t, "-db", db, "server", "set", "port", "0")
	do(t, "-db", db, "server", "set", "results", "2")
	do(t, "-db", db, "server", "set", "css", "/style.css")
	do(t, "-db", db, "server", "set", "info", "off")
	s := runServer(t, "-db", db, "server")
	code, body := s.get(t, "/search.json?q=the")
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var m map[string]any
	json.Unmarshal([]byte(body), &m)
	if m["itemsPerPage"] != float64(2) || len(m["hits"].([]any)) > 2 {
		t.Errorf("results 2: itemsPerPage %v, %d hits", m["itemsPerPage"], len(m["hits"].([]any)))
	}
	if code, _ := s.get(t, "/version"); code != 404 {
		t.Errorf("info off: /version is %d", code)
	}
	if _, html := s.get(t, "/search.html?q=otter"); !strings.Contains(html, `<link rel="stylesheet" href="/style.css">`) {
		t.Errorf("the css setting is not in the page:\n%.400s", html)
	}
}

func TestServerSaysOnStandardErrorWhenASetNeedsBuildingAndDoesNotBuildIt(t *testing.T) {
	t.Setenv("CMARKFIND_INDEX", "")
	db := filled(t)
	name := setNames(t, db)[0]
	dir := setIndexDir(t, db, name)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	s := runServer(t, "-db", db, "server", "port", "0")
	if e := s.stderr.String(); !strings.Contains(e, "indexer build") || !strings.Contains(e, "1 set") {
		t.Errorf("no start-up warning about the set that needs an index: %q", e)
	}
	if code, body := s.get(t, "/search.json?q=otter"); code != 503 || strings.Contains(body, "example.org/a.md") || !strings.Contains(body, "needs building") {
		t.Errorf("the only set has no index: %d %q, want 503 saying it needs building", code, body)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the server built the index: %v", err)
	}
}

func TestServerExitsWhenThePortIsInUseOrRefused(t *testing.T) {
	db := filled(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port
	o, e := &syncBuf{}, &syncBuf{}
	if code := RunContext(context.Background(), []string{"-db", db, "server", "port", fmt.Sprint(busy)}, o, e); code != 73 {
		t.Errorf("a port in use: exit %d, want 73\n%s", code, e)
	}
	if strings.Contains(o.String(), "listening") {
		t.Errorf("it said it was listening: %q", o.String())
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bind a low port")
	}
	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start"); err == nil && strings.TrimSpace(string(b)) == "0" {
		t.Skip("this system lets anyone bind a low port")
	}
	o, e = &syncBuf{}, &syncBuf{}
	if code := RunContext(context.Background(), []string{"-db", db, "server", "port", "81"}, o, e); code != 77 {
		t.Errorf("a refused port: exit %d, want 77\n%s", code, e)
	}
}
