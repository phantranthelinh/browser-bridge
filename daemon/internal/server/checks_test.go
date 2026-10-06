package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func TestNavigateURLChecks(t *testing.T) {
	h := newHarness(t, home.Config{BlockedHosts: []string{"bank.com"}})
	h.connect(fakeext.Options{})
	cases := []struct {
		url    string
		status int
		code   string
	}{
		{"chrome://settings", 200, protocol.ErrRestrictedURL},
		{"edge://settings", 200, protocol.ErrRestrictedURL},
		{"file:///C:/secret.txt", 200, protocol.ErrRestrictedURL},
		{"javascript:alert(1)", 200, protocol.ErrRestrictedURL},
		{"https://chromewebstore.google.com/detail/x", 200, protocol.ErrRestrictedURL},
		{"https://chrome.google.com/webstore/detail/x", 200, protocol.ErrRestrictedURL},
		{"kimi.com", 400, protocol.ErrInvalidRequest},
		{"https://bank.com/login", 200, protocol.ErrBlockedHost},
		{"https://www.bank.com", 200, protocol.ErrBlockedHost},
		{"https://BANK.com./", 200, protocol.ErrBlockedHost},
		{"https://bank.com:8443/", 200, protocol.ErrBlockedHost},
	}
	for _, c := range cases {
		status, resp := h.command(`{"action":"navigate","args":{"url":"` + c.url + `"},"session":"s1"}`)
		if status != c.status || resp.Error == nil || resp.Error.Code != c.code {
			t.Errorf("%s: want %d %s, got %d %+v", c.url, c.status, c.code, status, resp.Error)
		}
	}
	for _, ok := range []string{"https://notbank.com/", "https://bank.com.evil.net/", "http://localhost:3000/", "about:blank"} {
		if _, resp := h.command(`{"action":"navigate","args":{"url":"` + ok + `"},"session":"s1"}`); !resp.OK {
			t.Errorf("%s must reach the extension: %+v", ok, resp.Error)
		}
	}
}

func TestFindTabChecks(t *testing.T) {
	h := newHarness(t, home.Config{BlockedHosts: []string{"bank.com"}})
	h.connect(fakeext.Options{})
	status, resp := h.command(`{"action":"find_tab","args":{},"session":"s1"}`)
	expectError(t, status, resp, 400, protocol.ErrInvalidRequest)
	status, resp = h.command(`{"action":"find_tab","args":{"url":"www.bank.com/accounts"},"session":"s1"}`)
	expectError(t, status, resp, 200, protocol.ErrBlockedHost)
	for _, args := range []string{`{"url":"kimi.com"}`, `{"active":true}`, `{"url":"https://kimi.com/chat","active":true}`} {
		if _, resp := h.command(`{"action":"find_tab","args":` + args + `,"session":"s1"}`); !resp.OK {
			t.Errorf("%s: %+v", args, resp.Error)
		}
	}
}

func TestUploadPathChecks(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{})
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	body := func(paths ...string) string {
		b, _ := json.Marshal(map[string]any{"action": "upload", "session": "s1", "args": map[string]any{"selector": "#f", "files": paths}})
		return string(b)
	}
	for name, b := range map[string]string{
		"relative": body("a.txt"),
		"missing":  body(filepath.Join(dir, "missing.txt")),
		"folder":   body(dir),
		"one bad":  body(file, filepath.Join(dir, "missing.txt")),
	} {
		status, resp := h.command(b)
		if status != 400 || resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
			t.Errorf("%s: got %d %+v", name, status, resp.Error)
		}
	}
	if _, resp := h.command(body(file)); !resp.OK {
		t.Fatalf("existing file: %+v", resp.Error)
	}
}

func TestCDPBlocksBrowserAndTargetDomains(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{})
	for _, m := range []string{"Browser.close", "Target.createTarget"} {
		status, resp := h.command(`{"action":"cdp","args":{"method":"` + m + `"},"session":"s1"}`)
		expectError(t, status, resp, 200, protocol.ErrCDPError)
	}
	if _, resp := h.command(`{"action":"cdp","args":{"method":"Network.getAllCookies"},"session":"s1"}`); !resp.OK {
		t.Fatalf("cookie reads are an accepted risk (spec §7.1) and must pass: %+v", resp.Error)
	}
}

var onePixelPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func screenshotExt(mime string) fakeext.Handler {
	return func(protocol.RequestFrame) (protocol.Response, bool) {
		data, _ := json.Marshal(protocol.ScreenshotCapture{Data: base64.StdEncoding.EncodeToString(onePixelPNG), MimeType: mime, Width: 1, Height: 1})
		return protocol.Response{OK: true, Data: data}, true
	}
}

func TestScreenshotDefaultPath(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{Handler: screenshotExt("image/png")})
	_, resp := h.command(`{"action":"screenshot","session":"jira-report"}`)
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	var r protocol.ScreenshotResult
	json.Unmarshal(resp.Data, &r)
	if filepath.Dir(r.Path) != h.artifacts || !strings.HasPrefix(filepath.Base(r.Path), "jira-report-") || filepath.Ext(r.Path) != ".png" {
		t.Fatalf("path = %s", r.Path)
	}
	got, _ := os.ReadFile(r.Path)
	if !bytes.Equal(got, onePixelPNG) || r.SizeBytes != int64(len(onePixelPNG)) || r.MimeType != "image/png" || r.Width != 1 {
		t.Fatalf("result %+v does not match the file", r)
	}
	if strings.Contains(string(resp.Data), `"data"`) {
		t.Fatal("the base64 image must not be forwarded to the agent")
	}
}

func TestScreenshotJPEGGetsJpgExtension(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{Handler: screenshotExt("image/jpeg")})
	_, resp := h.command(`{"action":"screenshot","args":{"format":"jpeg"},"session":"s1"}`)
	var r protocol.ScreenshotResult
	json.Unmarshal(resp.Data, &r)
	if filepath.Ext(r.Path) != ".jpg" {
		t.Fatalf("path = %s", r.Path)
	}
}

func TestScreenshotExplicitPath(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{Handler: screenshotExt("image/png")})
	target := filepath.Join(t.TempDir(), "new", "folder", "shot.png")
	body, _ := json.Marshal(map[string]any{"action": "screenshot", "session": "s1", "args": map[string]any{"path": target}})
	for i := 0; i < 2; i++ { // the second run overwrites
		if _, resp := h.command(string(body)); !resp.OK {
			t.Fatalf("run %d: %+v", i, resp.Error)
		}
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, onePixelPNG) {
		t.Fatal("file not written")
	}
	status, resp := h.command(`{"action":"screenshot","args":{"path":"shot.png"},"session":"s1"}`)
	expectError(t, status, resp, 400, protocol.ErrInvalidRequest)
}

func TestScreenshotBadCaptureIsInternal(t *testing.T) {
	h := newHarness(t, home.Config{})
	h.connect(fakeext.Options{Handler: func(protocol.RequestFrame) (protocol.Response, bool) {
		return protocol.Response{OK: true, Data: json.RawMessage(`{"data":"%%%","mimeType":"image/png"}`)}, true
	}})
	status, resp := h.command(`{"action":"screenshot","session":"s1"}`)
	expectError(t, status, resp, 200, protocol.ErrInternal)
}
