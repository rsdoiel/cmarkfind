package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestFillBatchStopsAfterNPagesAndSaysHowManyRemain(t *testing.T) {
	srv, db := webSite(t), dbPath(t)
	r := do(t, "-db", db, "fill", "-pause", "0s", "-batch", "1", srv.URL)
	if r.code != 0 || !strings.Contains(r.out, "added 1") || !strings.Contains(r.out, "remaining 2") {
		t.Fatalf("%+v", r)
	}
	j := parseJSON(t, do(t, "-db", db, "fill", "-pause", "0s", "-batch", "1", "-json", srv.URL).out)
	if j["added"] != float64(1) || j["remaining"] != float64(1) || j["listed"] != float64(3) {
		t.Errorf("json %v", j)
	}
}

func TestSearchAndStatsSayWhatIsNotFetchedYet(t *testing.T) {
	srv, db := webSite(t), dbPath(t)
	do(t, "-db", db, "fill", "-pause", "0s", "-batch", "1", srv.URL)
	s := do(t, "-db", db, "search", "badgers")
	if !strings.Contains(s.errOut, "2 listed pages not fetched yet") {
		t.Errorf("search stderr %q", s.errOut)
	}
	if j := parseJSON(t, do(t, "-db", db, "search", "-json", "badgers").out); j["pending"] != float64(2) {
		t.Errorf("search json %v", j)
	}
	st := do(t, "-db", db, "stats")
	if !strings.Contains(st.out, "pending 2") {
		t.Errorf("stats %q", st.out)
	}
	if j := parseJSON(t, do(t, "-db", db, "stats", "-json").out); j["pending"] != float64(2) {
		t.Errorf("stats json %v", j)
	}
	do(t, "-db", db, "fill", "-pause", "0s", srv.URL)
	if s := do(t, "-db", db, "search", "badgers"); strings.Contains(s.errOut, "not fetched") {
		t.Errorf("nothing should be pending: %q", s.errOut)
	}
}

func TestTrickleUsageErrors(t *testing.T) {
	srv, db := webSite(t), dbPath(t)
	for name, args := range map[string][]string{
		"batch with a folder":        {"-db", db, "fill", "-batch", "5", localSite(t)},
		"negative batch":             {"-db", db, "fill", "-batch", "-1", srv.URL},
		"every without batch":        {"-db", db, "fill", "-every", "1s", srv.URL},
		"until-done without every":   {"-db", db, "fill", "-batch", "1", "-until-done", srv.URL},
		"every with a folder":        {"-db", db, "fill", "-batch", "1", "-every", "1s", localSite(t)},
		"every that is not positive": {"-db", db, "fill", "-batch", "1", "-every", "0s", srv.URL},
	} {
		if r := do(t, args...); r.code != 2 {
			t.Errorf("%s: exit %d\n%+v", name, r.code, r)
		}
	}
}

func TestEveryWithUntilDoneRunsBatchesUntilNothingRemains(t *testing.T) {
	srv, db := webSite(t), dbPath(t)
	r := do(t, "-db", db, "fill", "-pause", "0s", "-batch", "1", "-every", "1ms", "-until-done", srv.URL)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if n := strings.Count(r.out, "added 1"); n != 3 {
		t.Errorf("%d batches added a page each:\n%s", n, r.out)
	}
	if !strings.Contains(r.out, "remaining 0") {
		t.Errorf("out %q", r.out)
	}
	if s := do(t, "-db", db, "search", "badgers"); s.code != 0 || strings.Contains(s.errOut, "not fetched") {
		t.Errorf("%+v", s)
	}
	// JSON mode is one object per batch
	lines := strings.Split(strings.TrimSpace(do(t, "-db", dbPath(t), "fill", "-pause", "0s", "-batch", "1", "-every", "1ms", "-until-done", "-json", srv.URL).out), "\n")
	if len(lines) != 3 {
		t.Fatalf("json lines %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		parseJSON(t, l)
	}
}

// cancelOnWrite cancels a context at the first write, as Ctrl-C would between batches.
type cancelOnWrite struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	cancel context.CancelFunc
}

func (c *cancelOnWrite) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel()
	return c.buf.Write(p)
}

func TestInterruptingTheLoopEndsItCleanlyWithExitZero(t *testing.T) {
	srv, db := webSite(t), dbPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	w := &cancelOnWrite{cancel: cancel}
	var e bytes.Buffer
	code := RunContext(ctx, []string{"-db", db, "fill", "-pause", "0s", "-batch", "1", "-every", "1h", srv.URL}, w, &e)
	if code != 0 || !strings.Contains(w.buf.String(), "remaining 2") {
		t.Errorf("exit %d out %q err %q", code, w.buf.String(), e.String())
	}
}

// A site that is down for a while does not end the loop; the end carries the
// class of the first failure (workspace DR-0003, a bulk command).
func TestALoopThatMetAFailureExitsWithItsClass(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var o bytes.Buffer
	e := &cancelOnWrite{cancel: cancel}
	code := RunContext(ctx, []string{"-db", dbPath(t), "fill", "-pause", "0s", "-batch", "1", "-every", "1h", dead.URL}, &o, e)
	if code != 69 || !strings.HasPrefix(e.buf.String(), "cmarkfind: ") {
		t.Errorf("exit %d err %q", code, e.buf.String())
	}
}
