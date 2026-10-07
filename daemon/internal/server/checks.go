package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// precheck runs the checks that need no browser. Args have already passed the schema.
func (s *Server) precheck(action string, args json.RawMessage) *protocol.Error {
	switch action {
	case "navigate":
		var a protocol.NavigateArgs
		json.Unmarshal(args, &a)
		return s.checkNavigateURL(a.URL)
	case "find_tab":
		var a protocol.FindTabArgs
		json.Unmarshal(args, &a)
		if a.URL == "" && !a.Active {
			return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: "find_tab needs url, active:true, or both", Hint: schemaHint}
		}
		if a.URL != "" {
			host := hostOf(a.URL)
			if host == "" {
				return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("cannot read a host from %q", a.URL)}
			}
			if s.blocked(host) {
				return blockedError(host)
			}
		}
	case "upload":
		var a protocol.UploadArgs
		json.Unmarshal(args, &a)
		for _, f := range a.Files {
			if !filepath.IsAbs(f) {
				return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("%q is not an absolute path", f)}
			}
			st, err := os.Stat(f)
			if err != nil {
				return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("%s does not exist", f)}
			}
			if st.IsDir() {
				return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("%s is a folder, not a file", f)}
			}
		}
	case "screenshot":
		var a protocol.ScreenshotArgs
		json.Unmarshal(args, &a)
		if a.Path != "" && !filepath.IsAbs(a.Path) {
			// The daemon's working directory is not the agent's, so a relative path would land
			// somewhere the agent never looks.
			return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("path %q is not absolute", a.Path), Hint: "Pass an absolute path, or omit path to use ~/.browser-bridge/artifacts/"}
		}
	case "cdp":
		var a protocol.CDPArgs
		json.Unmarshal(args, &a)
		if strings.HasPrefix(a.Method, "Browser.") || strings.HasPrefix(a.Method, "Target.") {
			return &protocol.Error{Code: protocol.ErrCDPError, Message: a.Method + " is blocked: Browser.* and Target.* reach beyond the current tab"}
		}
	}
	return nil
}

func (s *Server) checkNavigateURL(raw string) *protocol.Error {
	if raw == "about:blank" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("%q is not an absolute URL", raw), Hint: "Include the scheme, e.g. https://" + raw}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return &protocol.Error{Code: protocol.ErrRestrictedURL, Message: scheme + ": URLs cannot be opened; only http, https and about:blank"}
	}
	host := normalizeHost(u.Hostname())
	if host == "" {
		return &protocol.Error{Code: protocol.ErrInvalidRequest, Message: fmt.Sprintf("%q has no host", raw)}
	}
	if isExtensionStore(host, u.Path) {
		return &protocol.Error{Code: protocol.ErrRestrictedURL, Message: "extensions cannot control extension store pages"}
	}
	if s.blocked(host) {
		return blockedError(host)
	}
	return nil
}

func isExtensionStore(host, path string) bool {
	return host == "chromewebstore.google.com" ||
		(host == "chrome.google.com" && strings.HasPrefix(path, "/webstore")) ||
		(host == "microsoftedge.microsoft.com" && strings.HasPrefix(path, "/addons"))
}

// hostOf reads the host from what an agent passes to find_tab: "example.com", "www.example.com/x" or
// a full URL.
func hostOf(s string) string {
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return normalizeHost(u.Hostname())
}

// normalizeHost lowercases and drops the trailing dot: https://bank.com./ is the same site as
// bank.com and must not slip past blockedHosts.
func normalizeHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

func (s *Server) blocked(host string) bool {
	for _, b := range s.cfg.BlockedHosts {
		if host == b || strings.HasSuffix(host, "."+b) {
			return true
		}
	}
	return false
}

func blockedError(host string) *protocol.Error {
	return &protocol.Error{Code: protocol.ErrBlockedHost, Message: host + " is in blockedHosts", Hint: "The user has blocked this site. Do not try to reach it another way"}
}

// saveScreenshot turns the extension's base64 capture into a file and answers with its path.
func (s *Server) saveScreenshot(session string, args json.RawMessage, resp protocol.Response) protocol.Response {
	var a protocol.ScreenshotArgs
	json.Unmarshal(args, &a)
	var c protocol.ScreenshotCapture
	if err := json.Unmarshal(resp.Data, &c); err != nil {
		return protocol.Fail(protocol.ErrInternal, "extension sent a malformed screenshot: "+err.Error(), "")
	}
	img, err := base64.StdEncoding.DecodeString(c.Data)
	if err != nil {
		return protocol.Fail(protocol.ErrInternal, "extension sent invalid base64: "+err.Error(), "")
	}
	size, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return protocol.Fail(protocol.ErrInternal, "extension sent an unreadable image: "+err.Error(), "")
	}
	path := a.Path
	if path == "" {
		ext := "png"
		if c.MimeType == "image/jpeg" {
			ext = "jpg"
		}
		path = filepath.Join(s.artifacts, fmt.Sprintf("%s-%s.%s", session, time.Now().Format("20060102-150405.000"), ext))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return protocol.Fail(protocol.ErrInternal, "cannot create folder for "+path+": "+err.Error(), "")
	}
	if err := os.WriteFile(path, img, 0o644); err != nil {
		return protocol.Fail(protocol.ErrInternal, "cannot write "+path+": "+err.Error(), "")
	}
	data, _ := json.Marshal(protocol.ScreenshotResult{Path: path, SizeBytes: int64(len(img)), MimeType: c.MimeType, Width: size.Width, Height: size.Height})
	return protocol.Response{OK: true, Data: data}
}
