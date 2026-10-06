package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

const listTabs = `{"action":"list_tabs","session":"s1"}`

func TestRejectsRequestsFromWebPages(t *testing.T) {
	h := newHarness(t, home.Config{})
	req, _ := http.NewRequest("POST", h.ts.URL+"/command", strings.NewReader(listTabs))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	status, resp := h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)

	req, _ = http.NewRequest("GET", h.ts.URL+"/status", nil)
	req.Header.Set("Origin", "https://evil.example")
	status, resp = h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)
}

func TestRejectsForeignHostHeader(t *testing.T) {
	h := newHarness(t, home.Config{})
	req, _ := http.NewRequest("GET", h.ts.URL+"/status", nil)
	req.Host = "evil.example:" + strings.Split(h.ts.Listener.Addr().String(), ":")[1]
	status, resp := h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)
}

func TestAcceptsLocalhostHostHeader(t *testing.T) {
	h := newHarness(t, home.Config{})
	req, _ := http.NewRequest("GET", h.ts.URL+"/status", nil)
	req.Host = "localhost:" + strings.Split(h.ts.Listener.Addr().String(), ":")[1]
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestCommandRequiresJSONContentType(t *testing.T) {
	h := newHarness(t, home.Config{})
	req, _ := http.NewRequest("POST", h.ts.URL+"/command", strings.NewReader(listTabs))
	req.Header.Set("Content-Type", "text/plain")
	status, resp := h.do(req)
	expectError(t, status, resp, 403, protocol.ErrForbidden)

	req, _ = http.NewRequest("POST", h.ts.URL+"/command", strings.NewReader(listTabs))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	status, _ = h.do(req)
	if status == 403 {
		t.Fatal("charset parameter must be accepted")
	}
}

func TestWebSocketOnlyForKnownExtension(t *testing.T) {
	h := newHarness(t, home.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fakeext.Dial(ctx, h.wsURL, fakeext.Options{ID: "abcdefghijklmnopabcdefghijklmnop"}); err == nil {
		t.Fatal("an unknown extension id must not connect")
	}
	if _, _, err := websocket.Dial(ctx, h.wsURL, nil); err == nil {
		t.Fatal("a client without Origin must not connect")
	}
	h.connect(fakeext.Options{})
}

func TestUnknownPathIsAnEnvelope(t *testing.T) {
	h := newHarness(t, home.Config{})
	req, _ := http.NewRequest("GET", h.ts.URL+"/nope", nil)
	status, resp := h.do(req)
	expectError(t, status, resp, 404, protocol.ErrInvalidRequest)
}
