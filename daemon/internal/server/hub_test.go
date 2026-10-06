package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// newTestHub serves only the hub, without the security middleware, so these tests exercise the
// WebSocket protocol alone.
func newTestHub(t *testing.T, blockedHosts []string) (*Hub, string) {
	t.Helper()
	h := NewHub(blockedHosts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	t.Cleanup(ts.Close)
	return h, "ws" + strings.TrimPrefix(ts.URL, "http")
}

func dialExt(t *testing.T, url string, opt fakeext.Options) *fakeext.Ext {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ext, err := fakeext.Dial(ctx, url, opt)
	if err != nil {
		t.Fatalf("fake extension could not connect: %v", err)
	}
	t.Cleanup(ext.Close)
	return ext
}

func hubDo(h *Hub, action, args string, timeout time.Duration) protocol.Response {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	return h.Do(ctx, "s1", action, json.RawMessage(args), deadline)
}

func waitDisconnected(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.Status().Connected {
		if time.Now().After(deadline) {
			t.Fatal("hub never noticed the disconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWelcomeCarriesBlockedHosts(t *testing.T) {
	_, url := newTestHub(t, []string{"bank.com"})
	ext := dialExt(t, url, fakeext.Options{})
	want := protocol.Welcome{Type: "welcome", ProtocolVersion: protocol.ProtocolVersion, DaemonVersion: protocol.Version, BlockedHosts: []string{"bank.com"}}
	if !reflect.DeepEqual(ext.Welcome, want) {
		t.Fatalf("welcome = %+v", ext.Welcome)
	}
}

func TestDoWithoutExtension(t *testing.T) {
	h, _ := newTestHub(t, nil)
	resp := hubDo(h, "list_tabs", `{}`, time.Second)
	if resp.Error == nil || resp.Error.Code != protocol.ErrExtensionNotConnected {
		t.Fatalf("got %+v", resp.Error)
	}
}

func TestDoRoundTrip(t *testing.T) {
	h, url := newTestHub(t, nil)
	ext := dialExt(t, url, fakeext.Options{})
	before := time.Now()
	resp := hubDo(h, "click", `{"selector":"@e3"}`, 5*time.Second)
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	var data struct {
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}
	json.Unmarshal(resp.Data, &data)
	if data.Action != "click" || string(data.Args) != `{"selector":"@e3"}` {
		t.Fatalf("data = %s", resp.Data)
	}
	f := <-ext.Requests
	if f.Session != "s1" || f.ID == "" {
		t.Errorf("frame = %+v", f)
	}
	if d := time.UnixMilli(f.Deadline); d.Before(before.Add(4*time.Second)) || d.After(time.Now().Add(5*time.Second)) {
		t.Errorf("deadline %v is not about now+5s", d)
	}
}

func TestExtensionErrorPassesThrough(t *testing.T) {
	h, url := newTestHub(t, nil)
	dialExt(t, url, fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		return protocol.Fail(protocol.ErrStaleRef, "@e12 is no longer in the page", "Take a new snapshot"), true
	}})
	resp := hubDo(h, "click", `{"selector":"@e12"}`, 5*time.Second)
	if resp.Error == nil || resp.Error.Code != protocol.ErrStaleRef || resp.Error.Hint != "Take a new snapshot" {
		t.Fatalf("got %+v", resp.Error)
	}
}

func TestDoTimesOutWhenExtensionNeverAnswers(t *testing.T) {
	h, url := newTestHub(t, nil)
	dialExt(t, url, fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		return protocol.Response{}, false
	}})
	start := time.Now()
	resp := hubDo(h, "click", `{}`, 200*time.Millisecond)
	if resp.Error == nil || resp.Error.Code != protocol.ErrTimeout {
		t.Fatalf("got %+v", resp.Error)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("timeout took %v", took)
	}
}

func TestLateResponseAfterTimeoutIsDropped(t *testing.T) {
	h, url := newTestHub(t, nil)
	release := make(chan struct{})
	dialExt(t, url, fakeext.Options{Handler: func(f protocol.RequestFrame) (protocol.Response, bool) {
		if f.Action == "click" {
			<-release
		}
		return protocol.Response{OK: true, Data: json.RawMessage(`{"from":"` + f.Action + `"}`)}, true
	}})
	if resp := hubDo(h, "click", `{}`, 100*time.Millisecond); resp.Error == nil || resp.Error.Code != protocol.ErrTimeout {
		t.Fatalf("got %+v", resp.Error)
	}
	close(release)
	resp := hubDo(h, "list_tabs", `{}`, 5*time.Second)
	if !resp.OK || !strings.Contains(string(resp.Data), "list_tabs") {
		t.Fatalf("next command got the stale answer: %s", resp.Data)
	}
}

func TestDisconnectFailsPendingCommands(t *testing.T) {
	h, url := newTestHub(t, nil)
	ext := dialExt(t, url, fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		return protocol.Response{}, false
	}})
	go func() {
		<-ext.Requests
		ext.Close()
	}()
	resp := hubDo(h, "click", `{}`, 10*time.Second)
	if resp.Error == nil || resp.Error.Code != protocol.ErrExtensionNotConnected {
		t.Fatalf("got %+v", resp.Error)
	}
}

func TestSecondExtensionIsRefusedWith4409(t *testing.T) {
	h, url := newTestHub(t, nil)
	dialExt(t, url, fakeext.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := fakeext.Dial(ctx, url, fakeext.Options{})
	if websocket.CloseStatus(err) != 4409 {
		t.Fatalf("second connection: %v", err)
	}
	if resp := hubDo(h, "list_tabs", `{}`, 5*time.Second); !resp.OK {
		t.Fatalf("first connection broken: %+v", resp.Error)
	}
}

func TestReconnectAfterDisconnect(t *testing.T) {
	h, url := newTestHub(t, nil)
	dialExt(t, url, fakeext.Options{}).Close()
	waitDisconnected(t, h)
	dialExt(t, url, fakeext.Options{})
	if resp := hubDo(h, "list_tabs", `{}`, 5*time.Second); !resp.OK {
		t.Fatalf("after reconnect: %+v", resp.Error)
	}
}

func TestSilentConnectionIsDroppedSoTheExtensionCanReconnect(t *testing.T) {
	h, url := newTestHub(t, nil)
	h.idleTimeout = 200 * time.Millisecond
	zombie := dialExt(t, url, fakeext.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := zombie.Closed(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("idle connection was not closed: %v", err)
	}
	waitDisconnected(t, h)
	dialExt(t, url, fakeext.Options{})
}

func TestPingGetsPong(t *testing.T) {
	_, url := newTestHub(t, nil)
	ext := dialExt(t, url, fakeext.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ext.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ext.Pongs:
	case <-ctx.Done():
		t.Fatal("no pong")
	}
}

func TestVersionMismatch(t *testing.T) {
	h, url := newTestHub(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := fakeext.Dial(ctx, url, fakeext.Options{ProtocolVersion: protocol.ProtocolVersion + 1})
	if websocket.CloseStatus(err) != 4400 {
		t.Fatalf("mismatched hello: %v", err)
	}
	resp := hubDo(h, "list_tabs", `{}`, time.Second)
	if resp.Error == nil || resp.Error.Code != protocol.ErrVersionMismatch || !strings.Contains(resp.Error.Hint, "daemon is older") {
		t.Fatalf("got %+v", resp.Error)
	}
	if st := h.Status(); st.Connected || st.ProtocolVersion != protocol.ProtocolVersion+1 {
		t.Fatalf("status must show the extension's version: %+v", st)
	}
	dialExt(t, url, fakeext.Options{})
	if resp := hubDo(h, "list_tabs", `{}`, 5*time.Second); !resp.OK {
		t.Fatalf("a matching extension must clear the mismatch: %+v", resp.Error)
	}
}
