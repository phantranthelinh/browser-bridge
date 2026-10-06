package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// lockedBuffer is a log sink that tests can read while the server writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	t         *testing.T
	srv       *Server
	ts        *httptest.Server
	wsURL     string
	log       *lockedBuffer
	artifacts string
}

func newHarness(t *testing.T, cfg home.Config) *harness {
	t.Helper()
	if cfg.ExtensionIDs == nil {
		cfg.ExtensionIDs = []string{protocol.DefaultExtensionID}
	}
	logBuf := &lockedBuffer{}
	artifacts := t.TempDir()
	srv := New(Options{Config: cfg, ArtifactsDir: artifacts, Log: slog.New(slog.NewTextHandler(logBuf, nil))})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{t: t, srv: srv, ts: ts, wsURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws", log: logBuf, artifacts: artifacts}
}

func (h *harness) connect(opt fakeext.Options) *fakeext.Ext {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ext, err := fakeext.Dial(ctx, h.wsURL, opt)
	if err != nil {
		h.t.Fatalf("fake extension could not connect: %v", err)
	}
	h.t.Cleanup(ext.Close)
	return ext
}

// command posts a body to /command and decodes the envelope.
func (h *harness) command(body string) (int, protocol.Response) {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.ts.URL+"/command", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return h.do(req)
}

func (h *harness) do(req *http.Request) (int, protocol.Response) {
	h.t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var resp protocol.Response
	if err := json.Unmarshal(b, &resp); err != nil {
		h.t.Fatalf("response is not an envelope: %s", b)
	}
	return res.StatusCode, resp
}

func (h *harness) getJSON(path string, v any) {
	h.t.Helper()
	res, err := http.Get(h.ts.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		h.t.Fatalf("GET %s: %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		h.t.Fatal(err)
	}
}

func expectError(t *testing.T, status int, resp protocol.Response, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus || resp.OK || resp.Error == nil || resp.Error.Code != wantCode {
		t.Fatalf("want %d %s, got %d %+v", wantStatus, wantCode, status, resp.Error)
	}
}
