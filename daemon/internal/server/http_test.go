package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func TestRequestValidation(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{})
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"broken json", `{"action":`, 400, protocol.ErrInvalidRequest},
		{"unknown top-level field", `{"action":"list_tabs","session":"s1","sesion":"x"}`, 400, protocol.ErrInvalidRequest},
		{"unknown action", `{"action":"hover","session":"s1"}`, 400, protocol.ErrUnknownAction},
		{"missing session", `{"action":"list_tabs"}`, 400, protocol.ErrInvalidRequest},
		{"session with space", `{"action":"list_tabs","session":"Jira Report"}`, 400, protocol.ErrInvalidRequest},
		{"session too long", `{"action":"list_tabs","session":"` + strings.Repeat("a", 65) + `"}`, 400, protocol.ErrInvalidRequest},
		{"timeout above max", `{"action":"list_tabs","session":"s1","timeoutMs":120001}`, 400, protocol.ErrInvalidRequest},
		{"negative timeout", `{"action":"list_tabs","session":"s1","timeoutMs":-1}`, 400, protocol.ErrInvalidRequest},
		{"schema violation", `{"action":"navigate","args":{},"session":"s1"}`, 400, protocol.ErrInvalidRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, resp := h.command(c.body)
			expectError(t, status, resp, c.status, c.code)
		})
	}
	if _, resp := h.command(`{"action":"list_tabs","session":"s1","timeoutMs":120000}`); !resp.OK {
		t.Fatalf("timeoutMs 120000 must be accepted: %+v", resp.Error)
	}
	_, resp := h.command(`{"action":"navigate","args":{},"session":"s1"}`)
	if !strings.Contains(resp.Error.Message, "url") || resp.Error.Hint == "" {
		t.Fatalf("schema error must name the field and point to /tools: %+v", resp.Error)
	}
}

func TestCommandWithoutExtension(t *testing.T) {
	h := newHarness(t, home.Config{})
	status, resp := h.command(listTabs)
	expectError(t, status, resp, 200, protocol.ErrExtensionNotConnected)
}

func TestCommandRoundTrip(t *testing.T) {
	h := newHarness(t, home.Config{})
	ext := h.connect(fakeext.Options{})
	status, resp := h.command(`{"action":"click","args":{"selector":"@e3"},"session":"jira-report","timeoutMs":5000}`)
	if status != 200 || !resp.OK || !strings.Contains(string(resp.Data), `"selector":"@e3"`) {
		t.Fatalf("%d %s %+v", status, resp.Data, resp.Error)
	}
	if f := <-ext.Requests; f.Session != "jira-report" || f.Action != "click" {
		t.Fatalf("frame = %+v", f)
	}
	// navigate's default timeout is 30s, not the 15s of other actions
	h.command(`{"action":"navigate","args":{"url":"https://a.com"},"session":"s1"}`)
	f := <-ext.Requests
	if left := time.Until(time.UnixMilli(f.Deadline)); left < 25*time.Second || left > 30*time.Second {
		t.Fatalf("navigate deadline is %v away, want about 30s", left)
	}
	// missing args reach the extension as {}
	h.command(listTabs)
	if f := <-ext.Requests; string(f.Args) != "{}" {
		t.Fatalf("args = %s", f.Args)
	}
}

func TestEmptyDataBecomesObject(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		return protocol.Response{OK: true}, true
	}})
	_, resp := h.command(`{"action":"network_stop","session":"s1"}`)
	if !resp.OK || string(resp.Data) != "{}" {
		t.Fatalf("data = %q", resp.Data)
	}
}

func TestSameSessionIsSerializedOtherSessionsAreNot(t *testing.T) {
	h := newHarness(t, home.Config{})
	gate := make(chan struct{})
	ext := h.connect(fakeext.Options{Handler: func(f protocol.RequestFrame) (protocol.Response, bool) {
		if f.Action == "snapshot" {
			<-gate
		}
		return protocol.Response{OK: true}, true
	}})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); h.command(`{"action":"snapshot","session":"a"}`) }()
	if f := <-ext.Requests; f.Action != "snapshot" {
		t.Fatalf("first request = %s", f.Action)
	}
	go func() { defer wg.Done(); h.command(`{"action":"list_tabs","session":"a"}`) }()
	if _, resp := h.command(`{"action":"list_tabs","session":"b"}`); !resp.OK {
		t.Fatalf("session b: %+v", resp.Error)
	}
	if f := <-ext.Requests; f.Session != "b" {
		t.Fatal("session a's second command reached the extension before its first finished")
	}
	select {
	case f := <-ext.Requests:
		t.Fatalf("unexpected request %s/%s while session a is busy", f.Session, f.Action)
	case <-time.After(100 * time.Millisecond):
	}
	close(gate)
	wg.Wait()
	if f := <-ext.Requests; f.Session != "a" || f.Action != "list_tabs" {
		t.Fatalf("queued command = %s/%s", f.Session, f.Action)
	}
}

func TestQueuedCommandTimesOutWithoutReachingExtension(t *testing.T) {
	h := newHarness(t, home.Config{})
	gate := make(chan struct{})
	defer close(gate)
	ext := h.connect(fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		<-gate
		return protocol.Response{OK: true}, true
	}})
	go h.command(`{"action":"snapshot","session":"a","timeoutMs":5000}`)
	<-ext.Requests
	status, resp := h.command(`{"action":"list_tabs","session":"a","timeoutMs":100}`)
	expectError(t, status, resp, 200, protocol.ErrTimeout)
	select {
	case f := <-ext.Requests:
		t.Fatalf("timed-out command still reached the extension: %s", f.Action)
	default:
	}
}

func TestLogNeverContainsSecrets(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{})
	h.command(`{"action":"fill","args":{"selector":"#pw","value":"hunter2-fill"},"session":"s1"}`)
	h.command(`{"action":"evaluate","args":{"code":"steal('evaluate-secret')"},"session":"s1"}`)
	h.command(`{"action":"handle_dialog","args":{"accept":true,"promptText":"prompt-secret"},"session":"s1"}`)
	h.command(`{"action":"navigate","args":{"url":"https://a.com/cb?token=url-secret"},"session":"s1"}`)
	log := h.log.String()
	for _, secret := range []string{"hunter2-fill", "evaluate-secret", "prompt-secret", "url-secret"} {
		if strings.Contains(log, secret) {
			t.Errorf("log contains %q:\n%s", secret, log)
		}
	}
	for _, want := range []string{"action=fill", "selector=#pw", "action=evaluate", "url=https://a.com/cb"} {
		if !strings.Contains(log, want) {
			t.Errorf("log is missing %q:\n%s", want, log)
		}
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, home.Config{})
	var st statusBody
	h.getJSON("/status", &st)
	addr := h.ts.Listener.Addr().String()
	if !st.Running || st.Version != protocol.Version || st.ProtocolVersion != protocol.ProtocolVersion || !strings.HasSuffix(addr, ":"+strconv.Itoa(st.Port)) || st.Extension.Connected {
		t.Fatalf("status = %+v", st)
	}
	h.connect(fakeext.Options{})
	h.command(`{"action":"list_tabs","session":"a"}`)
	h.command(`{"action":"list_tabs","session":"b"}`)
	h.getJSON("/status", &st)
	if !st.Extension.Connected || st.Extension.ID != protocol.DefaultExtensionID || st.Extension.Browser != "chrome" || st.Sessions != 2 {
		t.Fatalf("status = %+v", st)
	}
	h.command(`{"action":"close_session","session":"a"}`)
	h.getJSON("/status", &st)
	if st.Sessions != 1 {
		t.Fatalf("close_session must drop the session from the count: %d", st.Sessions)
	}
}

func TestTools(t *testing.T) {
	h := newHarness(t, home.Config{})
	var tools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	h.getJSON("/tools", &tools)
	if len(tools) != len(protocol.Actions) {
		t.Fatalf("%d tools, %d actions", len(tools), len(protocol.Actions))
	}
	for _, tl := range tools {
		if tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("%s: %+v", tl.Name, tl)
		}
		if tl.Name == "navigate" {
			req, _ := tl.InputSchema["required"].([]any)
			if len(req) != 1 || req[0] != "url" {
				t.Errorf("navigate must require url: %v", tl.InputSchema["required"])
			}
		}
	}
	var raw []json.RawMessage
	h.getJSON("/tools", &raw)
	if strings.Contains(string(raw[0]), "$schema") {
		t.Error("inputSchema must be inline, without $schema")
	}
}

func TestToolsMarkWhatTheConnectedExtensionImplements(t *testing.T) {
	h := newHarness(t, home.Config{})
	available := func() map[string]bool {
		var tools []struct {
			Name      string `json:"name"`
			Available *bool  `json:"available"`
		}
		h.getJSON("/tools", &tools)
		m := map[string]bool{}
		for _, tl := range tools {
			if tl.Available == nil {
				t.Fatalf("%s has no available field", tl.Name)
			}
			m[tl.Name] = *tl.Available
		}
		return m
	}
	for name, ok := range available() {
		if ok {
			t.Errorf("%s is available with no extension connected", name)
		}
	}
	ext := h.connect(fakeext.Options{Actions: []string{"navigate", "snapshot"}})
	got := available()
	if !got["navigate"] || !got["snapshot"] || got["click"] || len(got) != len(protocol.Actions) {
		t.Fatalf("available = %v", got)
	}
	ext.Close()
	waitDisconnected(t, h.srv.hub)
	if available()["navigate"] {
		t.Error("navigate stays available after the extension disconnected")
	}
}
