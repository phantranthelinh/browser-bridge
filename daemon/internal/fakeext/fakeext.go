// Package fakeext is a Go stand-in for the browser extension. It speaks the daemon's WebSocket
// protocol so the hub, the command pipeline and the CLI can be tested without a browser.
package fakeext

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// Handler answers one request. Returning reply=false sends nothing, which lets a test drive the
// daemon into TIMEOUT.
type Handler func(f protocol.RequestFrame) (resp protocol.Response, reply bool)

type Options struct {
	ID              string // defaults to protocol.DefaultExtensionID
	ProtocolVersion int    // defaults to protocol.ProtocolVersion
	Handler         Handler
}

type Ext struct {
	Welcome  protocol.Welcome
	Requests chan protocol.RequestFrame // every request received, in order
	Pongs    chan struct{}

	conn    *websocket.Conn
	handler Handler
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

// Echo answers every request with ok and data {"action": <action>, "args": <args>}.
func Echo(f protocol.RequestFrame) (protocol.Response, bool) {
	data, _ := json.Marshal(map[string]any{"action": f.Action, "args": f.Args})
	return protocol.Response{OK: true, Data: data}, true
}

// Dial connects to wsURL with the extension's Origin, sends hello and waits for welcome. If the
// daemon closes the socket instead, the returned error carries the close code
// (websocket.CloseStatus(err) gives 4400 or 4409).
func Dial(ctx context.Context, wsURL string, opt Options) (*Ext, error) {
	if opt.ID == "" {
		opt.ID = protocol.DefaultExtensionID
	}
	if opt.ProtocolVersion == 0 {
		opt.ProtocolVersion = protocol.ProtocolVersion
	}
	if opt.Handler == nil {
		opt.Handler = Echo
	}
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"chrome-extension://" + opt.ID}},
	})
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(64 << 20)
	hello := protocol.Hello{Type: "hello", ProtocolVersion: opt.ProtocolVersion, ExtensionVersion: protocol.Version, ExtensionID: opt.ID, Browser: "chrome"}
	if err := write(ctx, c, hello); err != nil {
		return nil, err
	}
	_, b, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	e := &Ext{Requests: make(chan protocol.RequestFrame, 100), Pongs: make(chan struct{}, 10), conn: c, handler: opt.Handler, done: make(chan struct{})}
	if err := json.Unmarshal(b, &e.Welcome); err != nil || e.Welcome.Type != "welcome" {
		c.CloseNow()
		return nil, fmt.Errorf("expected welcome, got %s", b)
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	go e.run()
	return e, nil
}

func (e *Ext) run() {
	defer close(e.done)
	for {
		_, b, err := e.conn.Read(e.ctx)
		if err != nil {
			e.mu.Lock()
			e.err = err
			e.mu.Unlock()
			return
		}
		var head struct {
			Type string `json:"type"`
		}
		json.Unmarshal(b, &head)
		switch head.Type {
		case "pong":
			e.Pongs <- struct{}{}
		case "request":
			var f protocol.RequestFrame
			json.Unmarshal(b, &f)
			e.Requests <- f
			go func() {
				resp, reply := e.handler(f)
				if !reply {
					return
				}
				write(e.ctx, e.conn, protocol.ResponseFrame{Type: "response", ID: f.ID, OK: resp.OK, Data: resp.Data, Error: resp.Error})
			}()
		}
	}
}

func (e *Ext) Ping(ctx context.Context) error {
	return write(ctx, e.conn, protocol.PingFrame{Type: "ping"})
}

// Send writes a raw frame, for tests of malformed or unexpected input.
func (e *Ext) Send(ctx context.Context, v any) error { return write(ctx, e.conn, v) }

// Close drops the connection the way a dying service worker does, without a close handshake.
func (e *Ext) Close() {
	e.cancel()
	e.conn.CloseNow()
	<-e.done
}

// Closed waits until the daemon closes the connection and returns the read error.
func (e *Ext) Closed(ctx context.Context) error {
	select {
	case <-e.done:
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func write(ctx context.Context, c *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}
