// Package client sends commands to a running daemon over its HTTP API, for bridge call and
// bridge mcp.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// A command can run for MaxTimeoutMs in the daemon after waiting its turn in the session's queue.
const httpTimeout = time.Duration(protocol.MaxTimeoutMs)*time.Millisecond + 30*time.Second

// The largest reply is a network body or an evaluate result of a few megabytes, escaped as JSON.
const maxReplyBytes = 64 << 20

type Client struct {
	Addr string
	HTTP *http.Client
}

func New(addr string) *Client {
	return &Client{Addr: addr, HTTP: &http.Client{Timeout: httpTimeout}}
}

// UnreachableError means no envelope came back: nothing listens at the address, the connection
// dropped, or whatever answered is not a bridge daemon.
type UnreachableError struct {
	Addr string
	Err  error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("cannot reach the bridge daemon at %s: %v", e.Addr, e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// Command runs one action. It returns the envelope and its raw JSON; an ok:false envelope is a
// result, not an error.
func (c *Client) Command(ctx context.Context, req protocol.Request) (protocol.Response, []byte, error) {
	var resp protocol.Response
	body, err := json.Marshal(req)
	if err != nil {
		return resp, nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.Addr+"/command", bytes.NewReader(body))
	if err != nil {
		return resp, nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(hr)
	if err != nil {
		return resp, nil, &UnreachableError{c.Addr, err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return resp, nil, &UnreachableError{c.Addr, err}
	}
	if json.Unmarshal(raw, &resp) != nil || (!resp.OK && resp.Error == nil) {
		return resp, nil, &UnreachableError{c.Addr, fmt.Errorf("answered %d without a bridge envelope", res.StatusCode)}
	}
	return resp, raw, nil
}
