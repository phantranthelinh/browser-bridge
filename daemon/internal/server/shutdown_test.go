package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func newShutdownHarness(t *testing.T) (*harness, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	logBuf := &lockedBuffer{}
	srv := New(Options{
		Config:       home.Config{ExtensionIDs: []string{protocol.DefaultExtensionID}},
		ArtifactsDir: t.TempDir(),
		Log:          slog.New(slog.NewTextHandler(logBuf, nil)),
		OnShutdown:   func() { calls.Add(1) },
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{t: t, srv: srv, ts: ts, log: logBuf}, &calls
}

func TestShutdownRepliesThenCallsTheHookOnce(t *testing.T) {
	h, calls := newShutdownHarness(t)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", h.ts.URL+"/shutdown", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		status, resp := h.do(req)
		if status != 200 || !resp.OK {
			t.Fatalf("status %d %+v", status, resp.Error)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("OnShutdown called %d times", calls.Load())
	}
}

func TestShutdownGoesThroughTheSecurityChecks(t *testing.T) {
	h, calls := newShutdownHarness(t)
	req, _ := http.NewRequest("POST", h.ts.URL+"/shutdown", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	status, resp := h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)

	req, _ = http.NewRequest("POST", h.ts.URL+"/shutdown", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	status, resp = h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)

	req, _ = http.NewRequest("GET", h.ts.URL+"/shutdown", nil)
	status, resp = h.do(req)
	expectError(t, status, resp, 404, protocol.ErrInvalidRequest)

	if calls.Load() != 0 {
		t.Fatal("a refused request must not stop the daemon")
	}
}

func TestStatusReportsThePID(t *testing.T) {
	h := newHarness(t, home.Config{})
	var st statusBody
	h.getJSON("/status", &st)
	if st.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", st.PID, os.Getpid())
	}
}
