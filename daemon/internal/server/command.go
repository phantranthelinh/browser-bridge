package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

var sessionRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// maxBodyBytes bounds a command body; fill values and evaluate code are the only large fields.
const maxBodyBytes = 10 << 20

const schemaHint = "GET /tools has the input schema of every action"

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req protocol.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeEnvelope(w, protocol.Fail(protocol.ErrInvalidRequest, "body is not a valid command: "+err.Error(), `Send {"action": ..., "args": {...}, "session": ...}`))
		return
	}
	resp := s.run(r.Context(), req)
	s.logCommand(req, resp, time.Since(start))
	writeEnvelope(w, resp)
}

// run takes a decoded command through validation, the daemon-side checks, the session queue and
// the extension, in that order. Every failure before the extension is cheap and needs no browser.
func (s *Server) run(ctx context.Context, req protocol.Request) protocol.Response {
	a, ok := protocol.Lookup(req.Action)
	if !ok {
		return protocol.Fail(protocol.ErrUnknownAction, fmt.Sprintf("unknown action %q", req.Action), "GET /tools lists the actions")
	}
	if !sessionRe.MatchString(req.Session) {
		return protocol.Fail(protocol.ErrInvalidRequest, "session must match "+sessionRe.String(), "Use a short lowercase name such as jira-report")
	}
	timeoutMs := a.DefaultTimeoutMs
	if req.TimeoutMs != 0 {
		if req.TimeoutMs < 0 || req.TimeoutMs > protocol.MaxTimeoutMs {
			return protocol.Fail(protocol.ErrInvalidRequest, fmt.Sprintf("timeoutMs must be between 1 and %d", protocol.MaxTimeoutMs), "")
		}
		timeoutMs = req.TimeoutMs
	}
	if t := bytes.TrimSpace(req.Args); len(t) == 0 || string(t) == "null" {
		req.Args = json.RawMessage("{}")
	}
	if err := protocol.ValidateArgs(a, req.Args); err != nil {
		return protocol.Fail(protocol.ErrInvalidRequest, err.Error(), schemaHint)
	}
	if e := s.precheck(a.Name, req.Args); e != nil {
		return protocol.Response{Error: e}
	}
	if e := s.hub.Ready(); e != nil {
		return protocol.Response{Error: e}
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	release, err := s.queues.Acquire(ctx, req.Session)
	if err != nil {
		return protocol.Fail(protocol.ErrTimeout, "an earlier command of session "+req.Session+" was still running when timeoutMs ran out", "Retry, or wait for the earlier command to finish")
	}
	defer release()

	resp := s.hub.Do(ctx, req.Session, a.Name, req.Args, deadline)
	if !resp.OK {
		return resp
	}
	switch a.Name {
	case "screenshot":
		resp = s.saveScreenshot(req.Session, req.Args, resp)
	case "close_session":
		s.queues.Forget(req.Session)
	}
	if len(bytes.TrimSpace(resp.Data)) == 0 || string(bytes.TrimSpace(resp.Data)) == "null" {
		resp.Data = json.RawMessage("{}")
	}
	return resp
}

// logCommand writes one line per command. Args are never logged as a whole: fill values,
// evaluate code and prompt text can hold passwords. Only the selector and the URL without its
// query string are kept.
func (s *Server) logCommand(req protocol.Request, resp protocol.Response, d time.Duration) {
	attrs := []any{"session", req.Session, "action", req.Action, "ms", d.Milliseconds(), "ok", resp.OK}
	var safe struct {
		Selector string `json:"selector"`
		URL      string `json:"url"`
	}
	json.Unmarshal(req.Args, &safe)
	if safe.Selector != "" {
		attrs = append(attrs, "selector", safe.Selector)
	}
	if u, err := url.Parse(safe.URL); safe.URL != "" && err == nil {
		attrs = append(attrs, "url", u.Scheme+"://"+u.Host+u.Path)
	}
	if resp.Error != nil {
		attrs = append(attrs, "code", resp.Error.Code)
	}
	s.log.Info("command", attrs...)
}
