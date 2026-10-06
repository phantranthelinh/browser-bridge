package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

const (
	closeVersionMismatch websocket.StatusCode = 4400
	closeAlreadyConnected websocket.StatusCode = 4409
	maxFrameBytes                             = 64 << 20
	helloTimeout                              = 5 * time.Second
	// The extension pings every 20s. Three missed pings means the service worker died without
	// closing the socket; dropping it lets the restarted worker connect instead of getting 4409.
	defaultIdleTimeout = 60 * time.Second
)

// ExtensionStatus is the "extension" object of GET /status.
type ExtensionStatus struct {
	Connected       bool   `json:"connected"`
	ID              string `json:"id,omitempty"`
	Version         string `json:"version,omitempty"`
	Browser         string `json:"browser,omitempty"`
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
}

// Hub holds the single extension connection and matches responses to the requests waiting on
// them.
type Hub struct {
	blockedHosts []string
	idleTimeout  time.Duration
	log          *slog.Logger

	mu       sync.Mutex
	conn     *websocket.Conn
	hello    protocol.Hello
	mismatch *protocol.Hello // set while the last extension that connected spoke another protocol version
	pending  map[string]chan protocol.Response
	nextID   uint64
}

func NewHub(blockedHosts []string, log *slog.Logger) *Hub {
	return &Hub{blockedHosts: blockedHosts, idleTimeout: defaultIdleTimeout, log: log, pending: map[string]chan protocol.Response{}}
}

// ServeWS handles GET /ws.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// InsecureSkipVerify only turns off coder/websocket's same-host Origin rule, which would reject
	// every chrome-extension:// origin. The security middleware has already matched Origin against
	// extensionIds; this is not TLS verification.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(maxFrameBytes)

	h.mu.Lock()
	busy := h.conn != nil
	h.mu.Unlock()
	if busy {
		c.Close(closeAlreadyConnected, "another extension is already connected")
		return
	}

	hello, err := readHello(r.Context(), c)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	if hello.ProtocolVersion != protocol.ProtocolVersion {
		h.mu.Lock()
		h.mismatch = &hello
		h.mu.Unlock()
		h.log.Warn("protocol version mismatch", "extension", hello.ProtocolVersion, "daemon", protocol.ProtocolVersion)
		c.Close(closeVersionMismatch, "protocol version mismatch")
		return
	}

	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		c.Close(closeAlreadyConnected, "another extension is already connected")
		return
	}
	h.conn, h.hello, h.mismatch = c, hello, nil
	h.mu.Unlock()
	h.log.Info("extension connected", "id", hello.ExtensionID, "version", hello.ExtensionVersion, "browser", hello.Browser)

	welcome := protocol.Welcome{Type: "welcome", ProtocolVersion: protocol.ProtocolVersion, DaemonVersion: protocol.Version, BlockedHosts: h.blockedHosts}
	if err := writeJSON(r.Context(), c, welcome); err != nil {
		h.drop(c, err)
		return
	}
	h.readLoop(r.Context(), c)
}

func readHello(ctx context.Context, c *websocket.Conn) (protocol.Hello, error) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()
	var hello protocol.Hello
	_, b, err := c.Read(ctx)
	if err != nil {
		return hello, fmt.Errorf("no hello: %w", err)
	}
	if err := json.Unmarshal(b, &hello); err != nil || hello.Type != "hello" {
		return hello, fmt.Errorf("first frame must be hello")
	}
	return hello, nil
}

func (h *Hub) readLoop(ctx context.Context, c *websocket.Conn) {
	for {
		readCtx, cancel := context.WithTimeout(ctx, h.idleTimeout)
		_, b, err := c.Read(readCtx)
		cancel()
		if err != nil {
			h.drop(c, err)
			return
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &head) != nil {
			h.log.Warn("unparseable frame from extension")
			continue
		}
		switch head.Type {
		case "response":
			var f protocol.ResponseFrame
			if json.Unmarshal(b, &f) != nil {
				h.log.Warn("bad response frame")
				continue
			}
			h.deliver(f)
		case "ping":
			if err := writeJSON(ctx, c, protocol.PongFrame{Type: "pong"}); err != nil {
				h.drop(c, err)
				return
			}
		case "event":
			var f protocol.EventFrame
			json.Unmarshal(b, &f)
			h.log.Info("extension event", "name", f.Name)
		default:
			h.log.Warn("unknown frame type", "type", head.Type)
		}
	}
}

func (h *Hub) deliver(f protocol.ResponseFrame) {
	h.mu.Lock()
	ch := h.pending[f.ID]
	delete(h.pending, f.ID)
	h.mu.Unlock()
	if ch == nil {
		return // already answered TIMEOUT
	}
	ch <- protocol.Response{OK: f.OK, Data: f.Data, Error: f.Error}
}

// drop forgets c and fails every request still waiting on it.
func (h *Hub) drop(c *websocket.Conn, cause error) {
	h.mu.Lock()
	if h.conn != c {
		h.mu.Unlock()
		return
	}
	h.conn = nil
	waiting := h.pending
	h.pending = map[string]chan protocol.Response{}
	h.mu.Unlock()
	c.CloseNow()
	h.log.Info("extension disconnected", "cause", cause)
	for _, ch := range waiting {
		ch <- protocol.Fail(protocol.ErrExtensionNotConnected, "the extension disconnected while running this command", "Check that the browser is open, then retry")
	}
}

// Ready reports why commands cannot be sent right now, or nil.
func (h *Hub) Ready() *protocol.Error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mismatch != nil {
		hint := "The extension is older than the daemon: rebuild it and reload it in chrome://extensions"
		if h.mismatch.ProtocolVersion > protocol.ProtocolVersion {
			hint = "The daemon is older than the extension: rebuild bridge and run bridge restart"
		}
		return &protocol.Error{
			Code:    protocol.ErrVersionMismatch,
			Message: fmt.Sprintf("extension speaks protocol %d, daemon speaks %d", h.mismatch.ProtocolVersion, protocol.ProtocolVersion),
			Hint:    hint,
		}
	}
	if h.conn == nil {
		return &protocol.Error{Code: protocol.ErrExtensionNotConnected, Message: "no browser extension is connected", Hint: "Open Chrome with the Browser Bridge extension enabled"}
	}
	return nil
}

// Do sends one command to the extension and waits for its answer until ctx ends.
func (h *Hub) Do(ctx context.Context, session, action string, args json.RawMessage, deadline time.Time) protocol.Response {
	if e := h.Ready(); e != nil {
		return protocol.Response{Error: e}
	}
	h.mu.Lock()
	c := h.conn
	if c == nil {
		h.mu.Unlock()
		return protocol.Fail(protocol.ErrExtensionNotConnected, "no browser extension is connected", "")
	}
	h.nextID++
	id := strconv.FormatUint(h.nextID, 10)
	ch := make(chan protocol.Response, 1)
	h.pending[id] = ch
	h.mu.Unlock()

	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	frame := protocol.RequestFrame{Type: "request", ID: id, Session: session, Action: action, Args: args, Deadline: deadline.UnixMilli()}
	if err := writeJSON(ctx, c, frame); err != nil {
		h.forget(id)
		if ctx.Err() != nil {
			return timeoutResponse(action)
		}
		h.drop(c, err)
		return protocol.Fail(protocol.ErrExtensionNotConnected, "could not send the command to the extension", "")
	}
	select {
	case resp := <-ch:
		return resp
	case <-ctx.Done():
		h.forget(id)
		return timeoutResponse(action)
	}
}

func (h *Hub) forget(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

func timeoutResponse(action string) protocol.Response {
	return protocol.Fail(protocol.ErrTimeout, action+" did not finish before timeoutMs", "Retry with a larger timeoutMs, or check the page with snapshot")
}

func (h *Hub) Status() ExtensionStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil {
		return ExtensionStatus{Connected: true, ID: h.hello.ExtensionID, Version: h.hello.ExtensionVersion, Browser: h.hello.Browser, ProtocolVersion: h.hello.ProtocolVersion}
	}
	if h.mismatch != nil {
		m := h.mismatch
		return ExtensionStatus{ID: m.ExtensionID, Version: m.ExtensionVersion, Browser: m.Browser, ProtocolVersion: m.ProtocolVersion}
	}
	return ExtensionStatus{}
}

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}
