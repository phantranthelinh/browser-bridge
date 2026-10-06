# Daemon Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go daemon `bridge serve`. The daemon receives commands over HTTP, validates them against a JSON Schema generated from Go structs, runs the security checks and the checks that need no browser, queues them per session, forwards them over WebSocket to the extension, and returns a standard envelope. All of it is tested with a fake extension written in Go.

**Architecture:** The `protocol` package is the single definition of the wire format: the args/result structs of each action, the WebSocket frames, and the error codes. The JSON Schema (used for validation, for `GET /tools`, and for the `schema/protocol.schema.json` file that the extension uses to generate TS types) is reflected from it. `server` contains the security middleware, the `/command` pipeline (validate → precheck → session queue → hub → post-processing), and the `Hub`, which holds exactly one extension connection. `home` handles `~/.browser-bridge`, and `session` handles the queues. `fakeext` plays the extension in tests.

**Tech Stack:** Go 1.27, `github.com/invopop/jsonschema` v0.14.0 (schema reflection), `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 (validation), `github.com/coder/websocket` v1.8.15, `log/slog`, `net/http` (ServeMux with method patterns).

**Spec:** `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§3, §4, §5, §6.1, §7, §9.1, §9.3, §9.4, §10, §11, §12 Go part)

**Out of scope for this plan:** the extension (plan 02 onward, written after the spike results are in), the subcommands `start`/`stop`/`status`/`logs`/`call`/`mcp`/`install-skill`, `SKILL.md` and the tool-table marker inside it, the root `check:gen` script (this plan uses a Go test in its place for the schema side).

## Global Constraints

- Go ≥ 1.27. Module path: `github.com/phantranthelinh/browser-bridge/daemon`, located in `daemon/`. Run every Go command from the repo root with `go -C daemon …`.
- Listen only on `127.0.0.1` or `localhost`. Default address `127.0.0.1:9876`. Precedence: `--addr` > `config.json` > default.
- `session` matches `^[a-z0-9][a-z0-9_-]{0,63}$`.
- `timeoutMs` is at most 120000. Default is 30000 for `navigate`/`reload`/`go_back`/`go_forward`, and 15000 for the remaining actions.
- HTTP status: 400 for `INVALID_REQUEST`/`UNKNOWN_ACTION`, 403 for `FORBIDDEN`, 200 for every other result including `ok:false`. The body is always the envelope `{ok, data?, error?: {code, message, hint?}}`, except `GET /tools` (an array) and `GET /status` (an object, §9.3).
- WebSocket: frames up to 64 MB; close with 4409 when an extension is already connected; close with 4400 on a `protocolVersion` mismatch.
- `Version = "0.1.0"`, `ProtocolVersion = 1`.
- The default extension ID is `nfjidhefdgblbbfhnmbcogkbphipngif`, derived from the public key in `extension/manifest-key.txt`.
- Tool descriptions and error messages are written in English, so that small models can read them too.
- Comments in code: no ticket codes; write one only when it explains something the code does not say by itself.
- Commit in the style the repo already uses: `feat(daemon): …`, `test(daemon): …`, `docs: …`.

## Review Focus

- **`config.json` saved with Notepad (with a BOM), a mistyped key name (`blockedHost`), or a URL written in place of a host (`https://bank.com/`):** the user will think the site is blocked when it is not. The daemon must refuse to start and name the file. Tests: `TestBadConfigIsAnErrorNamingTheFile`, `TestConfigOverridesAndNormalizesHosts` (Task 4).
- **A blocked host written in a different form:** `BANK.com.`, `www.bank.com`, `bank.com:8443` must still be blocked; `bank.com.evil.net` and `notbank.com` must not. Tests: `TestNavigateURLChecks`, `TestFindTabChecks` (Task 8).
- **A service worker that dies without closing the socket** (machine sleep, Chrome kills the SW): without an idle timeout, a "zombie" connection holds the slot and the restarted SW is answered with 4409 forever. Test: `TestSilentConnectionIsDroppedSoTheExtensionCanReconnect` (Task 6).
- **A response that arrives late after `TIMEOUT` was already returned:** that response must not fall into the session's next command. Test: `TestLateResponseAfterTimeoutIsDropped` (Task 6).
- **A relative path for `screenshot.path` or `upload.files`:** the daemon's working directory is not the agent's, so the file would end up where the agent never looks. The daemon must return `INVALID_REQUEST`. Tests: `TestScreenshotExplicitPath`, `TestUploadPathChecks` (Task 8).

---

### Task 1: Repo skeleton and the protocol envelope

**Files:**
- Create: `.gitattributes`
- Create: `.gitignore`
- Create: `extension/manifest-key.txt`
- Create: `daemon/go.mod` (qua `go mod init`)
- Create: `daemon/internal/protocol/protocol.go`
- Create: `daemon/internal/protocol/extid.go`
- Test: `daemon/internal/protocol/protocol_test.go`

**Interfaces:**
- Produces:
  - `protocol.Version string`, `protocol.ProtocolVersion int`, `protocol.DefaultExtensionID string`
  - `protocol.Request{Action string; Args json.RawMessage; Session string; TimeoutMs int}`
  - `protocol.Response{OK bool; Data json.RawMessage; Error *protocol.Error}`
  - `protocol.Error{Code, Message, Hint string}` (implement `error`)
  - `protocol.Fail(code, message, hint string) protocol.Response`
  - `protocol.HTTPStatus(e *protocol.Error) int`
  - Error code constants `protocol.ErrInvalidRequest` … `protocol.ErrInternal`, including `protocol.ErrForbidden`
  - `protocol.ExtensionIDFromKey(b64 string) (string, error)`

- [ ] **Step 1: Install Go**

```powershell
winget install --id GoLang.Go -e
```

Open a new terminal and run `go version`. Expected: `go version go1.27.x windows/amd64` or newer.

- [ ] **Step 2: Create `.gitattributes` and `.gitignore`**

This machine has `core.autocrlf=true`. Without `.gitattributes`, generated files (the schema) would be converted to CRLF on checkout.

`.gitattributes`:

```text
* text=auto eol=lf
*.png binary
*.jpg binary
```

`.gitignore`:

```text
/daemon/bridge.exe
node_modules/
```

- [ ] **Step 3: Create the extension's public key**

This key pins the extension ID when loading unpacked (spec §8.1). Only the public key is needed; Chrome does not need the private key for an unpacked extension.

`extension/manifest-key.txt` (one line):

```text
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAr+RhumvfLLC0HpMDXukXg1/91ERQp+2KnS0tfPyqlv66XjACR/SO5g/of7OlfVr2yFAPVS+RJUzE8C4QOjZdiYn/uu+5az5A+uaA6npQJPRIFnryOmFAV4/bH9cbp6Yho+x3sKE2xhrAUXV7ELGROsR9fIgXcayzGVm3LlUoJIeRjHgwWld2ftIIQSvhj0mPfw1llLz8SKIKucX3u0LpVxLn2W0FTNhMBhLIKVqX6lW1AI9FhvWWUlOVA/aVOE9M0sllWtCSPvdnDykXiF+3r2PQVFFWQa1DsGjMX9BzZNft0rs40YFtd0psq2qtN9lnm5ML/PyLljOv3eodhT4igQIDAQAB
```

- [ ] **Step 4: Initialize the module**

```bash
mkdir -p daemon/internal/protocol
go -C daemon mod init github.com/phantranthelinh/browser-bridge/daemon
```

Expected: creates `daemon/go.mod` with the line `go 1.27.x`.

- [ ] **Step 5: Write the failing test**

`daemon/internal/protocol/protocol_test.go`:

```go
package protocol

import (
	"encoding/json"
	"os"
	"testing"
)

func TestResponseEnvelopeOmitsEmptyFields(t *testing.T) {
	ok, _ := json.Marshal(Response{OK: true, Data: json.RawMessage(`{"a":1}`)})
	if string(ok) != `{"ok":true,"data":{"a":1}}` {
		t.Fatalf("got %s", ok)
	}
	fail, _ := json.Marshal(Fail(ErrStaleRef, "@e12 is no longer in the page", "Take a new snapshot"))
	want := `{"ok":false,"error":{"code":"STALE_REF","message":"@e12 is no longer in the page","hint":"Take a new snapshot"}}`
	if string(fail) != want {
		t.Fatalf("got %s", fail)
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := map[string]int{ErrInvalidRequest: 400, ErrUnknownAction: 400, ErrForbidden: 403, ErrStaleRef: 200, ErrTimeout: 200}
	for code, want := range cases {
		if got := HTTPStatus(&Error{Code: code}); got != want {
			t.Errorf("%s: got %d want %d", code, got, want)
		}
	}
	if HTTPStatus(nil) != 200 {
		t.Error("nil error must be 200")
	}
}

func TestDefaultExtensionIDMatchesManifestKey(t *testing.T) {
	key, err := os.ReadFile("../../../extension/manifest-key.txt")
	if err != nil {
		t.Fatal(err)
	}
	id, err := ExtensionIDFromKey(string(key))
	if err != nil {
		t.Fatal(err)
	}
	if id != DefaultExtensionID {
		t.Fatalf("manifest key gives %s, DefaultExtensionID is %s", id, DefaultExtensionID)
	}
}
```

- [ ] **Step 6: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/protocol/`
Expected: FAIL, compile errors `undefined: Response`, `undefined: Fail`, …

- [ ] **Step 7: Write `protocol.go`**

`daemon/internal/protocol/protocol.go`:

```go
// Package protocol is the single definition of the bridge wire format: the HTTP envelope, the
// argument and result types of every action, and the WebSocket frames between daemon and
// extension. The JSON Schema, the extension's TypeScript types, GET /tools and the MCP tool list
// are all generated from it, so a field is added here and nowhere else.
package protocol

import "encoding/json"

const (
	Version         = "0.1.0"
	ProtocolVersion = 1
	// DefaultExtensionID follows from the public key in extension/manifest-key.txt;
	// TestDefaultExtensionIDMatchesManifestKey fails if the two drift apart.
	DefaultExtensionID = "nfjidhefdgblbbfhnmbcogkbphipngif"
)

// Request is the body of POST /command.
type Request struct {
	Action    string          `json:"action"`
	Args      json.RawMessage `json:"args,omitempty"`
	Session   string          `json:"session"`
	TimeoutMs int             `json:"timeoutMs,omitempty"`
}

// Response is the envelope every endpoint answers with.
type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code" jsonschema:"enum=INVALID_REQUEST,enum=UNKNOWN_ACTION,enum=FORBIDDEN,enum=EXTENSION_NOT_CONNECTED,enum=VERSION_MISMATCH,enum=NO_CURRENT_TAB,enum=TAB_NOT_FOUND,enum=STALE_REF,enum=ELEMENT_NOT_FOUND,enum=AMBIGUOUS_SELECTOR,enum=ELEMENT_NOT_INTERACTABLE,enum=NAVIGATION_FAILED,enum=RESTRICTED_URL,enum=BLOCKED_HOST,enum=DIALOG_OPEN,enum=NO_DIALOG,enum=DETACHED_BY_USER,enum=TIMEOUT,enum=EVAL_ERROR,enum=CDP_ERROR,enum=INTERNAL"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Fail(code, message, hint string) Response {
	return Response{Error: &Error{Code: code, Message: message, Hint: hint}}
}

const (
	ErrInvalidRequest         = "INVALID_REQUEST"
	ErrUnknownAction          = "UNKNOWN_ACTION"
	ErrForbidden              = "FORBIDDEN"
	ErrExtensionNotConnected  = "EXTENSION_NOT_CONNECTED"
	ErrVersionMismatch        = "VERSION_MISMATCH"
	ErrNoCurrentTab           = "NO_CURRENT_TAB"
	ErrTabNotFound            = "TAB_NOT_FOUND"
	ErrStaleRef               = "STALE_REF"
	ErrElementNotFound        = "ELEMENT_NOT_FOUND"
	ErrAmbiguousSelector      = "AMBIGUOUS_SELECTOR"
	ErrElementNotInteractable = "ELEMENT_NOT_INTERACTABLE"
	ErrNavigationFailed       = "NAVIGATION_FAILED"
	ErrRestrictedURL          = "RESTRICTED_URL"
	ErrBlockedHost            = "BLOCKED_HOST"
	ErrDialogOpen             = "DIALOG_OPEN"
	ErrNoDialog               = "NO_DIALOG"
	ErrDetachedByUser         = "DETACHED_BY_USER"
	ErrTimeout                = "TIMEOUT"
	ErrEvalError              = "EVAL_ERROR"
	ErrCDPError               = "CDP_ERROR"
	ErrInternal               = "INTERNAL"
)

// HTTPStatus maps an error code to the status code of the HTTP response carrying it. Everything
// the daemon managed to process, including ok:false results from the extension, is 200.
func HTTPStatus(e *Error) int {
	if e == nil {
		return 200
	}
	switch e.Code {
	case ErrInvalidRequest, ErrUnknownAction:
		return 400
	case ErrForbidden:
		return 403
	}
	return 200
}
```

- [ ] **Step 8: Write `extid.go`**

`daemon/internal/protocol/extid.go`:

```go
package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// ExtensionIDFromKey derives a Chrome extension ID from the base64 public key in the manifest's
// "key" field: the first 128 bits of SHA-256 over the DER key, written as hex digits shifted to a-p.
func ExtensionIDFromKey(b64 string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	hx := hex.EncodeToString(sum[:16])
	id := make([]byte, len(hx))
	for i := 0; i < len(hx); i++ {
		c := hx[i]
		if c >= 'a' {
			id[i] = 'a' + 10 + (c - 'a')
		} else {
			id[i] = 'a' + (c - '0')
		}
	}
	return string(id), nil
}
```

- [ ] **Step 9: Run the test, confirm it passes**

Run: `go -C daemon test ./internal/protocol/`
Expected: `ok  github.com/phantranthelinh/browser-bridge/daemon/internal/protocol`

- [ ] **Step 10: Commit**

```bash
git add .gitattributes .gitignore extension/manifest-key.txt daemon/go.mod daemon/internal/protocol
git commit -m "feat(daemon): add protocol envelope, error codes and extension id"
```

---

### Task 2: Actions, WebSocket frames and schema validation

**Files:**
- Create: `daemon/internal/protocol/actions.go`
- Create: `daemon/internal/protocol/frames.go`
- Create: `daemon/internal/protocol/schema.go`
- Test: `daemon/internal/protocol/actions_test.go`
- Modify: `daemon/go.mod`, create `daemon/go.sum`

**Interfaces:**
- Consumes: `protocol.Error` (Task 1).
- Produces:
  - `protocol.Action{Name, Description string; Args, Result any; DefaultTimeoutMs int}`, `protocol.Actions []protocol.Action` (24 actions, in the order of §5), `protocol.Lookup(name string) (protocol.Action, bool)`, `protocol.MaxTimeoutMs = 120000`
  - Args/result structs for each action, for example `protocol.NavigateArgs{URL, NewTab, GroupTitle}`, `protocol.FindTabArgs{URL, Active}`, `protocol.UploadArgs{Selector, Files}`, `protocol.ScreenshotArgs{Format, Quality, Selector, FullPage, Path}`, `protocol.CDPArgs{Method, Params}`, `protocol.ScreenshotCapture{Data, MimeType, Width, Height}` (sent back by the extension), `protocol.ScreenshotResult{Path, SizeBytes, MimeType, Width, Height}` (received by the agent)
  - Frame: `protocol.Hello`, `protocol.Welcome{Type, ProtocolVersion, DaemonVersion, BlockedHosts}`, `protocol.RequestFrame{Type, ID, Session, Action, Args, Deadline int64}`, `protocol.ResponseFrame{Type, ID, OK, Data, Error}`, `protocol.EventFrame`, `protocol.PingFrame`, `protocol.PongFrame`
  - `protocol.InputSchema(a protocol.Action) json.RawMessage`: inline schema, with no `$schema`/`$id`/`$ref`
  - `protocol.Document() ([]byte, error)`: the content of `schema/protocol.schema.json`
  - `protocol.ValidateArgs(a protocol.Action, args json.RawMessage) error`: empty or `null` args are treated as `{}`

The spec's "exactly one of" constraints are encoded with `oneof_required` (`select`: `value`|`label`; `scroll`: `selector`|`direction`; `wait_for`: `selector`|`text`|`urlContains`|`load`). `JSONSchemaExtend` adds `dependentRequired` (`amount` needs `direction`, `state` needs `selector`) and `load: const true`.

- [ ] **Step 1: Write the failing test**

`daemon/internal/protocol/actions_test.go`:

```go
package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryActionHasUniqueNameAndTimeout(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Actions {
		if seen[a.Name] {
			t.Errorf("duplicate action %s", a.Name)
		}
		seen[a.Name] = true
		if a.DefaultTimeoutMs <= 0 || a.DefaultTimeoutMs > MaxTimeoutMs {
			t.Errorf("%s: bad default timeout %d", a.Name, a.DefaultTimeoutMs)
		}
		if a.Description == "" {
			t.Errorf("%s: missing description", a.Name)
		}
	}
	for _, name := range []string{"navigate", "reload", "go_back", "go_forward"} {
		if a, _ := Lookup(name); a.DefaultTimeoutMs != 30000 {
			t.Errorf("%s: want 30000, got %d", name, a.DefaultTimeoutMs)
		}
	}
	if a, _ := Lookup("click"); a.DefaultTimeoutMs != 15000 {
		t.Errorf("click: want 15000, got %d", a.DefaultTimeoutMs)
	}
	if _, ok := Lookup("hover"); ok {
		t.Error("hover is not an MVP action")
	}
}

func TestValidateArgs(t *testing.T) {
	cases := []struct {
		action, args string
		ok           bool
		errContains  string
	}{
		{"navigate", `{"url":"https://a.com"}`, true, ""},
		{"navigate", `{}`, false, "url"},
		{"navigate", `{"url":"https://a.com","bogus":1}`, false, "bogus"},
		{"list_tabs", ``, true, ""},
		{"list_tabs", `null`, true, ""},
		{"list_tabs", `{"x":1}`, false, "x"},
		{"fill", `{"selector":"@e1","value":""}`, true, ""},
		{"fill", `{"selector":"@e1"}`, false, "value"},
		{"click", `{"selector":""}`, false, "selector"},
		{"select", `{"selector":"#s","value":"a"}`, true, ""},
		{"select", `{"selector":"#s","label":"A"}`, true, ""},
		{"select", `{"selector":"#s","value":"a","label":"A"}`, false, ""},
		{"select", `{"selector":"#s"}`, false, ""},
		{"scroll", `{"direction":"down","amount":300}`, true, ""},
		{"scroll", `{"selector":"@e1"}`, true, ""},
		{"scroll", `{"amount":300}`, false, ""},
		{"scroll", `{"selector":"@e1","direction":"down"}`, false, ""},
		{"scroll", `{"direction":"sideways"}`, false, "direction"},
		{"wait_for", `{"selector":"#x","state":"hidden"}`, true, ""},
		{"wait_for", `{"text":"Done"}`, true, ""},
		{"wait_for", `{"load":true}`, true, ""},
		{"wait_for", `{"load":false}`, false, "load"},
		{"wait_for", `{"text":"a","urlContains":"b"}`, false, ""},
		{"wait_for", `{"text":"a","state":"hidden"}`, false, "state"},
		{"wait_for", `{}`, false, ""},
		{"screenshot", `{}`, true, ""},
		{"screenshot", `{"format":"jpeg","quality":101}`, false, "quality"},
		{"screenshot", `{"format":"gif"}`, false, "format"},
		{"upload", `{"selector":"#f","files":[]}`, false, "files"},
		{"handle_dialog", `{"accept":true}`, true, ""},
		{"handle_dialog", `{}`, false, "accept"},
		{"cdp", `{"method":"DOM.getDocument","params":{"depth":1}}`, true, ""},
		{"snapshot", `{"maxChars":0}`, false, "maxChars"},
		{"navigate", `[1,2]`, false, ""},
		{"navigate", `{"url":`, false, "JSON"},
	}
	for _, c := range cases {
		a, ok := Lookup(c.action)
		if !ok {
			t.Fatalf("unknown action %s", c.action)
		}
		err := ValidateArgs(a, json.RawMessage(c.args))
		if c.ok && err != nil {
			t.Errorf("%s %s: unexpected error %v", c.action, c.args, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s %s: expected an error", c.action, c.args)
		}
		if err != nil && c.errContains != "" && !strings.Contains(err.Error(), c.errContains) {
			t.Errorf("%s %s: error %q should mention %q", c.action, c.args, err, c.errContains)
		}
	}
}

func TestValidationErrorHidesSchemaURL(t *testing.T) {
	a, _ := Lookup("navigate")
	err := ValidateArgs(a, json.RawMessage(`{}`))
	if strings.Contains(err.Error(), "mem:///") {
		t.Fatalf("error leaks schema url: %v", err)
	}
}

func TestDocumentHasEveryActionType(t *testing.T) {
	b, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"NavigateArgs", "TabResult", "WaitForArgs", "ScreenshotCapture", "Hello", "Welcome", "RequestFrame", "ResponseFrame", "Error", "ActionName"} {
		if _, ok := doc.Defs[name]; !ok {
			t.Errorf("$defs is missing %s", name)
		}
	}
	if !strings.Contains(string(doc.Defs["Welcome"]), "blockedHosts") {
		t.Error("Welcome must carry blockedHosts")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/protocol/`
Expected: FAIL, `undefined: Actions`, `undefined: Lookup`, `undefined: ValidateArgs`, `undefined: Document`

- [ ] **Step 3: Add the dependencies**

```bash
go -C daemon get github.com/invopop/jsonschema@v0.14.0 github.com/santhosh-tekuri/jsonschema/v6@v6.0.3
```

- [ ] **Step 4: Write `actions.go`**

`daemon/internal/protocol/actions.go`:

```go
package protocol

import "github.com/invopop/jsonschema"

// Action describes one command an agent can send. Args and Result are zero values of the structs
// that get reflected into JSON Schema; Result is what the agent receives in "data".
type Action struct {
	Name             string
	Description      string
	Args             any
	Result           any
	DefaultTimeoutMs int
}

const (
	navigationTimeoutMs = 30000
	actionTimeoutMs     = 15000
	MaxTimeoutMs        = 120000
)

type NoArgs struct{}

type EmptyResult struct{}

type NavigateArgs struct {
	URL        string `json:"url" jsonschema:"minLength=1" jsonschema_description:"http(s) URL or about:blank"`
	NewTab     bool   `json:"newTab,omitempty" jsonschema_description:"Open a new background tab instead of reusing the current tab"`
	GroupTitle string `json:"groupTitle,omitempty" jsonschema_description:"Tab group title, used only when the group is created. Defaults to the session name"`
}

type TabResult struct {
	TabID int    `json:"tabId"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type FindTabArgs struct {
	URL    string `json:"url,omitempty" jsonschema_description:"Host to match: kimi.com also matches www.kimi.com. Path is ignored"`
	Active bool   `json:"active,omitempty" jsonschema_description:"Borrow the tab the user is looking at instead of searching the session's tabs"`
}

type FindTabResult struct {
	TabID    int    `json:"tabId"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Borrowed bool   `json:"borrowed"`
}

type ListTabsResult struct {
	Tabs []TabEntry `json:"tabs"`
}

type TabEntry struct {
	TabID    int    `json:"tabId"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Current  bool   `json:"current"`
	Borrowed bool   `json:"borrowed"`
}

type CloseTabResult struct {
	Closed   bool `json:"closed"`
	Released bool `json:"released"`
}

type CloseSessionResult struct {
	Closed int `json:"closed"`
}

type PageResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type SnapshotArgs struct {
	MaxChars int `json:"maxChars,omitempty" jsonschema:"minimum=1,default=20000"`
}

type SnapshotResult struct {
	URL       string      `json:"url"`
	Title     string      `json:"title"`
	Tree      string      `json:"tree"`
	Frames    []FrameInfo `json:"frames"`
	Truncated bool        `json:"truncated"`
}

type FrameInfo struct {
	Frame  int    `json:"frame" jsonschema_description:"0-based index of the iframe in document order"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type SelectorArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
}

type Dialog struct {
	Type    string `json:"type" jsonschema:"enum=alert,enum=confirm,enum=prompt,enum=beforeunload"`
	Message string `json:"message"`
}

type ClickResult struct {
	Tag    string  `json:"tag"`
	Text   string  `json:"text"`
	Dialog *Dialog `json:"dialog,omitempty"`
}

type FillArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
	Value    string `json:"value" jsonschema_description:"Text to put in the field. Empty string clears it"`
}

type FillResult struct {
	Mode string `json:"mode" jsonschema:"enum=value,enum=contenteditable"`
}

type SelectArgs struct {
	Selector string `json:"selector" jsonschema:"minLength=1" jsonschema_description:"Element ref from snapshot (@e12) or a CSS selector that matches exactly one element"`
	Value    string `json:"value,omitempty" jsonschema:"oneof_required=byValue" jsonschema_description:"Option value. Give exactly one of value or label"`
	Label    string `json:"label,omitempty" jsonschema:"oneof_required=byLabel" jsonschema_description:"Visible option text. Give exactly one of value or label"`
}

type SelectResult struct {
	Selected SelectedOption `json:"selected"`
}

type SelectedOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type PressKeyArgs struct {
	Key      string `json:"key" jsonschema:"minLength=1" jsonschema_description:"Enter, Escape, Tab, ArrowDown, Control+A ..."`
	Selector string `json:"selector,omitempty" jsonschema_description:"Focus this element first"`
}

type PressKeyResult struct {
	Dialog *Dialog `json:"dialog,omitempty"`
}

type ScrollArgs struct {
	Selector  string `json:"selector,omitempty" jsonschema:"oneof_required=toElement" jsonschema_description:"Scroll this element into view. Give either selector or direction"`
	Direction string `json:"direction,omitempty" jsonschema:"oneof_required=byAmount,enum=up,enum=down,enum=left,enum=right"`
	Amount    int    `json:"amount,omitempty" jsonschema:"minimum=1,default=600" jsonschema_description:"Pixels, only together with direction"`
}

// JSONSchemaExtend forbids amount without direction.
func (ScrollArgs) JSONSchemaExtend(s *jsonschema.Schema) {
	s.DependentRequired = map[string][]string{"amount": {"direction"}}
}

type ScrollResult struct {
	ScrollX float64 `json:"scrollX"`
	ScrollY float64 `json:"scrollY"`
}

type UploadArgs struct {
	Selector string   `json:"selector" jsonschema:"minLength=1" jsonschema_description:"An input[type=file], as a ref or a CSS selector"`
	Files    []string `json:"files" jsonschema:"minItems=1" jsonschema_description:"Absolute paths of existing files"`
}

type UploadResult struct {
	FileCount int `json:"fileCount"`
}

type WaitForArgs struct {
	Selector    string `json:"selector,omitempty" jsonschema:"oneof_required=selector" jsonschema_description:"Wait for this element to reach state"`
	State       string `json:"state,omitempty" jsonschema:"enum=visible,enum=hidden,default=visible"`
	Text        string `json:"text,omitempty" jsonschema:"oneof_required=text" jsonschema_description:"Wait until the page text contains this"`
	URLContains string `json:"urlContains,omitempty" jsonschema:"oneof_required=urlContains"`
	Load        bool   `json:"load,omitempty" jsonschema:"oneof_required=load" jsonschema_description:"Wait for the load event. Must be true"`
}

// JSONSchemaExtend pins load to true and allows state only next to selector.
func (WaitForArgs) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("load"); ok {
		p.Const = true
	}
	s.DependentRequired = map[string][]string{"state": {"selector"}}
}

type WaitForResult struct {
	Matched   bool `json:"matched"`
	ElapsedMs int  `json:"elapsedMs"`
}

type ScreenshotArgs struct {
	Format   string `json:"format,omitempty" jsonschema:"enum=png,enum=jpeg,default=png"`
	Quality  int    `json:"quality,omitempty" jsonschema:"minimum=0,maximum=100,default=80" jsonschema_description:"JPEG quality, ignored for png"`
	Selector string `json:"selector,omitempty" jsonschema_description:"Capture only this element"`
	FullPage bool   `json:"fullPage,omitempty"`
	Path     string `json:"path,omitempty" jsonschema_description:"Absolute file path to write. Parent folders are created and an existing file is overwritten. Default: ~/.browser-bridge/artifacts/"`
}

// ScreenshotCapture is what the extension sends back. The daemon writes the image to disk and
// answers the agent with ScreenshotResult instead.
type ScreenshotCapture struct {
	Data     string `json:"data" jsonschema_description:"Base64-encoded image bytes"`
	MimeType string `json:"mimeType" jsonschema:"enum=image/png,enum=image/jpeg"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type ScreenshotResult struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	MimeType  string `json:"mimeType"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type NetworkFilterArgs struct {
	Filter string `json:"filter,omitempty" jsonschema_description:"Only requests whose URL contains this"`
}

type NetworkRequestsResult struct {
	Capturing bool                    `json:"capturing"`
	Count     int                     `json:"count"`
	Requests  []NetworkRequestSummary `json:"requests"`
}

type NetworkRequestSummary struct {
	RequestID string `json:"requestId"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status"`
	MimeType  string `json:"mimeType"`
	Completed bool   `json:"completed"`
}

type NetworkRequestDetailArgs struct {
	RequestID string `json:"requestId" jsonschema:"minLength=1"`
}

type NetworkRequestDetailResult struct {
	Request           NetworkRequestInfo   `json:"request"`
	Response          *NetworkResponseInfo `json:"response,omitempty"`
	Body              string               `json:"body"`
	BodyBase64Encoded bool                 `json:"bodyBase64Encoded"`
	BodyError         string               `json:"bodyError,omitempty"`
}

type NetworkRequestInfo struct {
	URL      string            `json:"url"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
	PostData string            `json:"postData,omitempty"`
}

type NetworkResponseInfo struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	MimeType string            `json:"mimeType"`
}

type HandleDialogArgs struct {
	Accept     bool   `json:"accept"`
	PromptText string `json:"promptText,omitempty" jsonschema_description:"Text to type into a prompt() dialog"`
}

type EvaluateArgs struct {
	Code string `json:"code" jsonschema:"minLength=1" jsonschema_description:"JavaScript run in the page's main world. Top-level await is allowed"`
}

type EvaluateResult struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type CDPArgs struct {
	Method string         `json:"method" jsonschema:"minLength=1" jsonschema_description:"CDP method such as DOM.getDocument. Browser.* and Target.* are blocked"`
	Params map[string]any `json:"params,omitempty"`
}

var Actions = []Action{
	{"navigate", "Open a URL in the session's current tab, or in a new background tab. Waits for the load event.", NavigateArgs{}, TabResult{}, navigationTimeoutMs},
	{"find_tab", "Make an existing tab the session's current tab: search the session's tabs by host, or borrow the tab the user is looking at.", FindTabArgs{}, FindTabResult{}, actionTimeoutMs},
	{"list_tabs", "List the session's tabs.", NoArgs{}, ListTabsResult{}, actionTimeoutMs},
	{"close_tab", "Close the current tab. A borrowed tab is only released, never closed.", NoArgs{}, CloseTabResult{}, actionTimeoutMs},
	{"close_session", "Close every tab of the session, release borrowed tabs and remove the tab group.", NoArgs{}, CloseSessionResult{}, actionTimeoutMs},
	{"go_back", "Go back in the current tab's history.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"go_forward", "Go forward in the current tab's history.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"reload", "Reload the current tab.", NoArgs{}, PageResult{}, navigationTimeoutMs},
	{"snapshot", "Read the page as an accessibility tree. Interactive elements get refs like @e12 to use as selector.", SnapshotArgs{}, SnapshotResult{}, actionTimeoutMs},
	{"click", "Click an element with a real mouse event.", SelectorArgs{}, ClickResult{}, actionTimeoutMs},
	{"fill", "Replace the text of an input, textarea or rich text editor.", FillArgs{}, FillResult{}, actionTimeoutMs},
	{"select", "Choose an option in a <select> by value or by visible label.", SelectArgs{}, SelectResult{}, actionTimeoutMs},
	{"press_key", "Press a key or key combination, optionally after focusing an element.", PressKeyArgs{}, PressKeyResult{}, actionTimeoutMs},
	{"scroll", "Scroll an element into view, or scroll the page in a direction.", ScrollArgs{}, ScrollResult{}, actionTimeoutMs},
	{"upload", "Set the files of an input[type=file].", UploadArgs{}, UploadResult{}, actionTimeoutMs},
	{"wait_for", "Wait until an element is visible or hidden, the page contains a text, the URL contains a string, or the page has loaded.", WaitForArgs{}, WaitForResult{}, actionTimeoutMs},
	{"screenshot", "Capture the visible tab, the full page or one element to an image file.", ScreenshotArgs{}, ScreenshotResult{}, actionTimeoutMs},
	{"network_start", "Start recording the current tab's network requests, discarding earlier ones.", NetworkFilterArgs{}, EmptyResult{}, actionTimeoutMs},
	{"network_requests", "List recorded network requests.", NetworkFilterArgs{}, NetworkRequestsResult{}, actionTimeoutMs},
	{"network_request_detail", "Headers and body of one recorded request.", NetworkRequestDetailArgs{}, NetworkRequestDetailResult{}, actionTimeoutMs},
	{"network_stop", "Stop recording and discard recorded requests.", NoArgs{}, EmptyResult{}, actionTimeoutMs},
	{"handle_dialog", "Accept or dismiss the open alert, confirm or prompt dialog.", HandleDialogArgs{}, Dialog{}, actionTimeoutMs},
	{"evaluate", "Run JavaScript in the page and return the result.", EvaluateArgs{}, EvaluateResult{}, actionTimeoutMs},
	{"cdp", "Send a raw Chrome DevTools Protocol command to the current tab.", CDPArgs{}, map[string]any{}, actionTimeoutMs},
}

func Lookup(name string) (Action, bool) {
	for _, a := range Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}
```

- [ ] **Step 5: Write `frames.go`**

`daemon/internal/protocol/frames.go`:

```go
package protocol

import "encoding/json"

// WebSocket frames between daemon and extension. Every frame is a JSON text message whose "type"
// field names the frame.

type Hello struct {
	Type             string `json:"type" jsonschema:"enum=hello"`
	ProtocolVersion  int    `json:"protocolVersion"`
	ExtensionVersion string `json:"extensionVersion"`
	ExtensionID      string `json:"extensionId"`
	Browser          string `json:"browser" jsonschema:"enum=chrome,enum=edge"`
}

type Welcome struct {
	Type            string   `json:"type" jsonschema:"enum=welcome"`
	ProtocolVersion int      `json:"protocolVersion"`
	DaemonVersion   string   `json:"daemonVersion"`
	BlockedHosts    []string `json:"blockedHosts"`
}

type RequestFrame struct {
	Type    string          `json:"type" jsonschema:"enum=request"`
	ID      string          `json:"id"`
	Session string          `json:"session"`
	Action  string          `json:"action"`
	Args    json.RawMessage `json:"args"`
	// Deadline is epoch milliseconds; past it the daemon has already answered TIMEOUT.
	Deadline int64 `json:"deadline"`
}

type ResponseFrame struct {
	Type  string          `json:"type" jsonschema:"enum=response"`
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

type EventFrame struct {
	Type string          `json:"type" jsonschema:"enum=event"`
	Name string          `json:"name" jsonschema:"enum=tab.closed,enum=dialog.opened,enum=debugger.detached"`
	Data json.RawMessage `json:"data,omitempty"`
}

type PingFrame struct {
	Type string `json:"type" jsonschema:"enum=ping"`
}

type PongFrame struct {
	Type string `json:"type" jsonschema:"enum=pong"`
}
```

- [ ] **Step 6: Write `schema.go`**

`daemon/internal/protocol/schema.go`:

```go
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/invopop/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaFor reflects a Go value into an inline JSON Schema with no $schema, $id or $ref, so it can
// be embedded as a tool's inputSchema or as one entry of $defs.
func SchemaFor(v any) *jsonschema.Schema {
	r := &jsonschema.Reflector{ExpandedStruct: true, DoNotReference: true}
	s := r.Reflect(v)
	s.Version = ""
	s.ID = ""
	return s
}

// InputSchema is the JSON Schema of an action's args.
func InputSchema(a Action) json.RawMessage {
	b, err := json.Marshal(SchemaFor(a.Args))
	if err != nil {
		panic(err)
	}
	return b
}

// frameTypes are the non-action types the extension needs TypeScript definitions for.
var frameTypes = []any{
	Hello{}, Welcome{}, RequestFrame{}, ResponseFrame{}, EventFrame{}, PingFrame{}, PongFrame{},
	Error{}, ScreenshotCapture{},
}

// Document renders schema/protocol.schema.json: one $defs entry per named Go type, keyed by the
// type name, so json-schema-to-typescript emits interfaces with the same names as the Go structs.
func Document() ([]byte, error) {
	defs := map[string]*jsonschema.Schema{}
	add := func(v any) {
		name := reflect.TypeOf(v).Name()
		if name == "" {
			return
		}
		s := SchemaFor(v)
		s.Title = name
		defs[name] = s
	}
	actionNames := make([]string, 0, len(Actions))
	for _, a := range Actions {
		add(a.Args)
		add(a.Result)
		actionNames = append(actionNames, a.Name)
	}
	for _, v := range frameTypes {
		add(v)
	}
	defs["ActionName"] = &jsonschema.Schema{Title: "ActionName", Type: "string", Enum: toAny(actionNames)}

	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "BrowserBridgeProtocol",
		"$defs":   defs,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

var (
	compileOnce sync.Once
	compiled    map[string]*validator.Schema
)

func compileAll() {
	compiled = map[string]*validator.Schema{}
	c := validator.NewCompiler()
	for _, a := range Actions {
		doc, err := validator.UnmarshalJSON(bytes.NewReader(InputSchema(a)))
		if err != nil {
			panic(err)
		}
		url := "mem:///" + a.Name + ".json"
		if err := c.AddResource(url, doc); err != nil {
			panic(err)
		}
		s, err := c.Compile(url)
		if err != nil {
			panic(fmt.Sprintf("schema for %s: %v", a.Name, err))
		}
		compiled[a.Name] = s
	}
}

// ValidateArgs checks args against the action's schema. Missing args count as {}. The returned
// error message is meant for the agent, so it names the offending field and nothing else.
func ValidateArgs(a Action, args json.RawMessage) error {
	compileOnce.Do(compileAll)
	if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
		args = json.RawMessage("{}")
	}
	inst, err := validator.UnmarshalJSON(bytes.NewReader(args))
	if err != nil {
		return fmt.Errorf("args is not valid JSON: %v", err)
	}
	err = compiled[a.Name].Validate(inst)
	if err == nil {
		return nil
	}
	ve, ok := err.(*validator.ValidationError)
	if !ok {
		return err
	}
	// The first line names the schema URL, which means nothing to an agent.
	lines := strings.Split(strings.TrimSpace(ve.Error()), "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "-"))
	}
	return fmt.Errorf("invalid args for %s: %s", a.Name, strings.Join(lines, "; "))
}
```

- [ ] **Step 7: Run the test, confirm it passes**

```bash
go -C daemon mod tidy
go -C daemon test ./internal/protocol/
```

Expected: `ok  github.com/phantranthelinh/browser-bridge/daemon/internal/protocol`

A validation error message will look like `invalid args for navigate: at '': missing property 'url'`. It must not contain `mem:///`.

- [ ] **Step 8: Commit**

```bash
git add daemon/go.mod daemon/go.sum daemon/internal/protocol
git commit -m "feat(daemon): define actions and frames, validate args with JSON Schema"
```

---

### Task 3: `schemagen` and the committed schema file

**Files:**
- Create: `daemon/cmd/schemagen/main.go`
- Test: `daemon/cmd/schemagen/main_test.go`
- Create: `schema/protocol.schema.json` (sinh ra)

**Interfaces:**
- Consumes: `protocol.Document()` (Task 2).
- Produces: `schema/protocol.schema.json` with `$defs` named after the Go types (`NavigateArgs`, `TabResult`, `Hello`, `ActionName`, …). The extension plan reads this file with `json-schema-to-typescript`. Command to regenerate: `go -C daemon run ./cmd/schemagen`.

- [ ] **Step 1: Write the failing test**

`daemon/cmd/schemagen/main_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// The committed schema is what the extension's TypeScript types are generated from, so a Go
// change that is not followed by a schemagen run must fail the build.
func TestCommittedSchemaIsUpToDate(t *testing.T) {
	want, err := protocol.Document()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../../schema/protocol.schema.json")
	if err != nil {
		t.Fatalf("%v (run: go -C daemon run ./cmd/schemagen)", err)
	}
	// A Windows checkout with core.autocrlf turns LF into CRLF.
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatal("schema/protocol.schema.json is stale (run: go -C daemon run ./cmd/schemagen)")
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./cmd/schemagen/`
Expected: FAIL `open ../../../schema/protocol.schema.json: The system cannot find the file specified. (run: go -C daemon run ./cmd/schemagen)`

- [ ] **Step 3: Write `main.go`**

`daemon/cmd/schemagen/main.go`:

```go
// Command schemagen writes schema/protocol.schema.json from the protocol package.
// Run it from the daemon folder: go run ./cmd/schemagen
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func main() {
	out := flag.String("out", filepath.Join("..", "schema", "protocol.schema.json"), "output file")
	flag.Parse()
	b, err := protocol.Document()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(*out), 0o755)
	}
	if err == nil {
		err = os.WriteFile(*out, b, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
```

- [ ] **Step 4: Generate the schema, then run the test**

```bash
go -C daemon run ./cmd/schemagen
go -C daemon test ./cmd/schemagen/
```

Expected: prints `wrote ../schema/protocol.schema.json`, test `ok`. Open the file and check: `$defs.Welcome.properties` has `blockedHosts`, and `$defs.ActionName.enum` has 24 elements.

- [ ] **Step 5: Confirm the test catches a stale schema**

Temporarily change any `jsonschema_description` in `actions.go`, then run `go -C daemon test ./cmd/schemagen/`. Expected: FAIL `schema/protocol.schema.json is stale`. Revert that change and run again; it passes.

- [ ] **Step 6: Commit**

```bash
git add daemon/cmd/schemagen schema/protocol.schema.json
git commit -m "feat(daemon): generate schema/protocol.schema.json and fail tests when stale"
```

---

### Task 4: Home directory, config, runtime files and log

**Files:**
- Create: `daemon/internal/home/home.go`
- Test: `daemon/internal/home/home_test.go`

**Interfaces:**
- Consumes: `protocol.DefaultExtensionID` (Task 1).
- Produces:
  - `home.Config{Addr string; BlockedHosts []string; ExtensionIDs []string}` (tag JSON `addr`, `blockedHosts`, `extensionIds`)
  - `home.DefaultAddr = "127.0.0.1:9876"`
  - `home.Dir() (string, error)`: `%USERPROFILE%\.browser-bridge`; the environment variable `BRIDGE_HOME` overrides it (used for tests)
  - `home.ArtifactsDir(dir string) string`
  - `home.LoadConfig(dir string) (home.Config, error)`: a missing file falls back to the defaults; hosts in `BlockedHosts` are lowercased and trimmed; `ExtensionIDs`/`BlockedHosts` are never `nil`
  - `home.ValidateAddr(addr string) error`, `home.ResolveAddr(flagAddr string, cfg home.Config) (string, error)`
  - `home.WriteRuntimeFiles(dir string, pid int, addr string) error`, `home.RemoveRuntimeFiles(dir string)`
  - `home.OpenLog(dir string) (*os.File, error)`: renames the old `logs/daemon.log` to `daemon.log.prev`

- [ ] **Step 1: Write the failing test**

`daemon/internal/home/home_test.go`:

```go
package home

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMissingConfigGivesDefaults(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Addr: "127.0.0.1:9876", BlockedHosts: []string{}, ExtensionIDs: []string{protocol.DefaultExtensionID}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got %+v", cfg)
	}
}

func TestConfigOverridesAndNormalizesHosts(t *testing.T) {
	dir := writeConfig(t, "\xef\xbb\xbf"+`{"addr":"localhost:9000","blockedHosts":[" Bank.COM "]}`)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "localhost:9000" || !reflect.DeepEqual(cfg.BlockedHosts, []string{"bank.com"}) {
		t.Fatalf("got %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.ExtensionIDs, []string{protocol.DefaultExtensionID}) {
		t.Fatalf("extensionIds default lost: %+v", cfg.ExtensionIDs)
	}
}

func TestBadConfigIsAnErrorNamingTheFile(t *testing.T) {
	cases := map[string]string{
		"syntax":        `{"addr": }`,
		"unknown field": `{"blockedHost": ["bank.com"]}`,
		"public addr":   `{"addr": "0.0.0.0:9876"}`,
		"url as host":   `{"blockedHosts": ["https://bank.com/"]}`,
		"wildcard host": `{"blockedHosts": ["*.bank.com"]}`,
	}
	for name, content := range cases {
		dir := writeConfig(t, content)
		_, err := LoadConfig(dir)
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if !strings.Contains(err.Error(), filepath.Join(dir, "config.json")) {
			t.Errorf("%s: error %q does not name the file", name, err)
		}
	}
}

func TestResolveAddr(t *testing.T) {
	cfg := Config{Addr: "127.0.0.1:9000"}
	if a, _ := ResolveAddr("", cfg); a != "127.0.0.1:9000" {
		t.Errorf("config addr not used: %s", a)
	}
	if a, _ := ResolveAddr("localhost:9100", cfg); a != "localhost:9100" {
		t.Errorf("flag addr not used: %s", a)
	}
	if _, err := ResolveAddr("192.168.1.5:9876", cfg); err == nil {
		t.Error("non-loopback flag addr must be rejected")
	}
}

func TestRuntimeFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRuntimeFiles(dir, 1234, "127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	pid, _ := os.ReadFile(filepath.Join(dir, "daemon.pid"))
	addr, _ := os.ReadFile(filepath.Join(dir, "daemon.addr"))
	if string(pid) != "1234" || string(addr) != "127.0.0.1:9876" {
		t.Fatalf("pid=%q addr=%q", pid, addr)
	}
	RemoveRuntimeFiles(dir)
	if _, err := os.Stat(filepath.Join(dir, "daemon.pid")); !os.IsNotExist(err) {
		t.Fatal("daemon.pid not removed")
	}
}

func TestOpenLogKeepsPreviousRun(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("first run\n")
	f.Close()
	f, err = OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	prev, _ := os.ReadFile(filepath.Join(dir, "logs", "daemon.log.prev"))
	if string(prev) != "first run\n" {
		t.Fatalf("prev log = %q", prev)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/home/`
Expected: FAIL, `undefined: LoadConfig`, …

- [ ] **Step 3: Write `home.go`**

`daemon/internal/home/home.go`:

```go
// Package home owns ~/.browser-bridge: config.json, the daemon.pid/daemon.addr files, the daemon
// log and the artifacts folder.
package home

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

const DefaultAddr = "127.0.0.1:9876"

type Config struct {
	Addr         string   `json:"addr"`
	BlockedHosts []string `json:"blockedHosts"`
	ExtensionIDs []string `json:"extensionIds"`
}

// Dir is %USERPROFILE%\.browser-bridge. BRIDGE_HOME overrides it so tests never touch the real one.
func Dir() (string, error) {
	if d := os.Getenv("BRIDGE_HOME"); d != "" {
		return d, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".browser-bridge"), nil
}

func ArtifactsDir(dir string) string { return filepath.Join(dir, "artifacts") }

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// LoadConfig reads dir/config.json. A missing file means defaults. A file that does not parse, has
// an unknown field or an invalid value is an error naming the file: falling back to defaults
// would silently drop the user's blockedHosts.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{Addr: DefaultAddr, BlockedHosts: []string{}, ExtensionIDs: []string{protocol.DefaultExtensionID}}
	path := filepath.Join(dir, "config.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	// Notepad saves UTF-8 with a BOM, which encoding/json rejects.
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := ValidateAddr(cfg.Addr); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	hosts := make([]string, 0, len(cfg.BlockedHosts))
	for _, h := range cfg.BlockedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !hostRe.MatchString(h) {
			return Config{}, fmt.Errorf("%s: blockedHosts entry %q must be a bare host such as bank.com (subdomains are matched automatically)", path, h)
		}
		hosts = append(hosts, h)
	}
	cfg.BlockedHosts = hosts
	if cfg.ExtensionIDs == nil {
		cfg.ExtensionIDs = []string{}
	}
	return cfg, nil
}

// ValidateAddr accepts only loopback addresses: the daemon must never be reachable from another
// machine, whatever config.json or --addr says.
func ValidateAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("addr %q: host must be 127.0.0.1 or localhost", addr)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("addr %q: port must be 1-65535", addr)
	}
	return nil
}

// ResolveAddr applies the precedence --addr > config.json > default.
func ResolveAddr(flagAddr string, cfg Config) (string, error) {
	if flagAddr == "" {
		return cfg.Addr, nil
	}
	return flagAddr, ValidateAddr(flagAddr)
}

func WriteRuntimeFiles(dir string, pid int, addr string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "daemon.addr"), []byte(addr), 0o644)
}

func RemoveRuntimeFiles(dir string) {
	os.Remove(filepath.Join(dir, "daemon.pid"))
	os.Remove(filepath.Join(dir, "daemon.addr"))
}

// OpenLog moves the previous run's log to daemon.log.prev and starts a fresh daemon.log.
func OpenLog(dir string) (*os.File, error) {
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return nil, err
	}
	cur := filepath.Join(logs, "daemon.log")
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, cur+".prev"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(cur, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go -C daemon test ./internal/home/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/home
git commit -m "feat(daemon): load config strictly and manage pid, addr and log files"
```

---

### Task 5: Per-session queue

**Files:**
- Create: `daemon/internal/session/queue.go`
- Test: `daemon/internal/session/queue_test.go`

**Interfaces:**
- Produces:
  - `session.New() *session.Queues`
  - `(*Queues).Acquire(ctx context.Context, session string) (release func(), err error)`: waits for its turn; if `ctx` expires it returns `ctx.Err()`; calling `release` twice does no harm
  - `(*Queues).Forget(session string)`, `(*Queues).Count() int`: the number of sessions that have sent a command and not yet `close_session`

- [ ] **Step 1: Write the failing test**

`daemon/internal/session/queue_test.go`:

```go
package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitUsers(t *testing.T, q *Queues, session string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for q.users(session) != n {
		if time.Now().After(deadline) {
			t.Fatalf("session %s: want %d users, have %d", session, n, q.users(session))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSameSessionRunsInArrivalOrder(t *testing.T) {
	q := New()
	ctx := context.Background()
	release, err := q.Acquire(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []int
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.Acquire(ctx, "s")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			r()
		}()
		waitUsers(t, q, "s", i+1)
		// users is counted just before the goroutine blocks on the channel; give it time to block
		// so the next goroutine really arrives after it.
		time.Sleep(10 * time.Millisecond)
	}
	release()
	wg.Wait()
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("order = %v", order)
	}
}

func TestDifferentSessionsRunInParallel(t *testing.T) {
	q := New()
	ra, _ := q.Acquire(context.Background(), "a")
	defer ra()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	rb, err := q.Acquire(ctx, "b")
	if err != nil {
		t.Fatalf("session b blocked by session a: %v", err)
	}
	rb()
}

func TestWaitingPastDeadlineGivesUp(t *testing.T) {
	q := New()
	r, _ := q.Acquire(context.Background(), "s")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := q.Acquire(ctx, "s")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	r()
	waitUsers(t, q, "s", 0)
	// the abandoned waiter must not hold the turn
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	r2, err := q.Acquire(ctx2, "s")
	if err != nil {
		t.Fatalf("queue stuck after a waiter timed out: %v", err)
	}
	r2()
}

func TestReleaseTwiceIsHarmless(t *testing.T) {
	q := New()
	r, _ := q.Acquire(context.Background(), "s")
	r()
	r()
	if q.users("s") != 0 {
		t.Fatal("double release corrupted the queue")
	}
}

func TestCountAndForget(t *testing.T) {
	q := New()
	for _, s := range []string{"a", "b", "a"} {
		r, _ := q.Acquire(context.Background(), s)
		r()
	}
	if q.Count() != 2 {
		t.Fatalf("Count = %d", q.Count())
	}
	q.Forget("a")
	if q.Count() != 1 {
		t.Fatalf("Count after Forget = %d", q.Count())
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/session/`
Expected: FAIL, `undefined: New`

- [ ] **Step 3: Write `queue.go`**

`daemon/internal/session/queue.go`:

```go
// Package session serializes commands per session: commands of one session run one at a time in
// arrival order, different sessions run in parallel.
package session

import (
	"context"
	"sync"
)

type Queues struct {
	mu    sync.Mutex
	m     map[string]*queue
	known map[string]struct{}
}

type queue struct {
	// turn has capacity 1; the goroutine whose send succeeded is the one running. Blocked senders
	// on a Go channel are woken in FIFO order, which gives arrival-order execution.
	turn  chan struct{}
	users int // running plus waiting; the entry is dropped when it reaches 0
}

func New() *Queues {
	return &Queues{m: map[string]*queue{}, known: map[string]struct{}{}}
}

// Acquire waits for the session's turn. If ctx ends first it returns ctx.Err(); otherwise the
// caller must call release exactly once.
func (q *Queues) Acquire(ctx context.Context, session string) (release func(), err error) {
	q.mu.Lock()
	s := q.m[session]
	if s == nil {
		s = &queue{turn: make(chan struct{}, 1)}
		q.m[session] = s
	}
	s.users++
	q.known[session] = struct{}{}
	q.mu.Unlock()

	select {
	case s.turn <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-s.turn
				q.leave(session, s)
			})
		}, nil
	case <-ctx.Done():
		q.leave(session, s)
		return nil, ctx.Err()
	}
}

func (q *Queues) leave(session string, s *queue) {
	q.mu.Lock()
	defer q.mu.Unlock()
	s.users--
	if s.users == 0 && q.m[session] == s {
		delete(q.m, session)
	}
}

// Forget drops a session from Count after close_session succeeded.
func (q *Queues) Forget(session string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.known, session)
}

// Count is the number of sessions that have sent a command and not been closed.
func (q *Queues) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.known)
}

func (q *Queues) users(session string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if s := q.m[session]; s != nil {
		return s.users
	}
	return 0
}
```

- [ ] **Step 4: Run the test several times, confirm it passes stably**

Run: `go -C daemon test -count=5 ./internal/session/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/session
git commit -m "feat(daemon): serialize commands per session in arrival order"
```

---

### Task 6: Fake extension and the WebSocket hub

**Files:**
- Create: `daemon/internal/fakeext/fakeext.go`
- Create: `daemon/internal/server/hub.go`
- Test: `daemon/internal/server/hub_test.go`

**Interfaces:**
- Consumes: the frames and `protocol.Response`/`protocol.Fail` (Task 1, 2).
- Produces:
  - `fakeext.Dial(ctx context.Context, wsURL string, opt fakeext.Options) (*fakeext.Ext, error)`; `fakeext.Options{ID string; ProtocolVersion int; Handler fakeext.Handler}`; `fakeext.Handler func(f protocol.RequestFrame) (resp protocol.Response, reply bool)`; `fakeext.Echo`; the fields and methods `Ext.Welcome`, `Ext.Requests chan protocol.RequestFrame`, `Ext.Pongs`, `Ext.Ping(ctx)`, `Ext.Send(ctx, v)`, `Ext.Close()`, `Ext.Closed(ctx) error`. When the daemon rejects the connection, the returned error carries the close code: `websocket.CloseStatus(err)` yields 4400 or 4409.
  - `server.NewHub(blockedHosts []string, log *slog.Logger) *server.Hub`
  - `(*Hub).ServeWS(w, r)`: the handler for `GET /ws` (the Origin has already been checked by the middleware in Task 7)
  - `(*Hub).Ready() *protocol.Error`: `nil`, `VERSION_MISMATCH` (the hint says which side is older) or `EXTENSION_NOT_CONNECTED`
  - `(*Hub).Do(ctx, session, action string, args json.RawMessage, deadline time.Time) protocol.Response`: if `ctx` expires it returns `TIMEOUT`
  - `(*Hub).Status() server.ExtensionStatus{Connected, ID, Version, Browser, ProtocolVersion}`
  - Field `idleTimeout` (default 60s) so tests can shorten it

- [ ] **Step 1: Add the dependency**

```bash
mkdir -p daemon/internal/fakeext daemon/internal/server
go -C daemon get github.com/coder/websocket@v1.8.15
```

- [ ] **Step 2: Write the fake extension**

This is a test tool (spec §12), not the code under test, so it is written first. `daemon/internal/fakeext/fakeext.go`:

```go
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
```

- [ ] **Step 3: Write the failing test for the hub**

`daemon/internal/server/hub_test.go`:

```go
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
```

- [ ] **Step 4: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/server/`
Expected: FAIL, `undefined: NewHub`, `undefined: Hub`

- [ ] **Step 5: Write `hub.go`**

`daemon/internal/server/hub.go`:

```go
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
```

- [ ] **Step 6: Run the test several times, confirm it passes stably**

```bash
go -C daemon mod tidy
go -C daemon test -count=3 ./internal/server/
```

Expected: `ok`

- [ ] **Step 7: Commit**

```bash
git add daemon/go.mod daemon/go.sum daemon/internal/fakeext daemon/internal/server
git commit -m "feat(daemon): add WebSocket hub for the extension and a Go fake extension"
```

---

### Task 7: HTTP server: security, `/command`, `/tools`, `/status`

**Files:**
- Create: `daemon/internal/server/server.go`
- Create: `daemon/internal/server/command.go`
- Create: `daemon/internal/server/checks.go` (minimal version; Task 8 replaces it entirely)
- Test: `daemon/internal/server/harness_test.go`
- Test: `daemon/internal/server/security_test.go`
- Test: `daemon/internal/server/http_test.go`

**Interfaces:**
- Consumes: `home.Config` (Task 4), `session.Queues` (Task 5), `Hub` (Task 6), `protocol.ValidateArgs`/`InputSchema`/`Lookup` (Task 2).
- Produces:
  - `server.Options{Config home.Config; ArtifactsDir string; Log *slog.Logger}`, `server.New(opt server.Options) *server.Server`, `(*Server).Handler() http.Handler`
  - Unexported method `(*Server).precheck(action string, args json.RawMessage) *protocol.Error`: Task 8 fills in its content
  - Pipeline order in `run`: lookup → session regex → `timeoutMs` → schema → `precheck` → `hub.Ready` → queue → `hub.Do` → per-action post-processing → empty `data` becomes `{}`
  - Log one `slog` line per command: `session`, `action`, `ms`, `ok`, `selector`, `url` (query string stripped), `code`. Never log the full args.

- [ ] **Step 1: Write the shared test harness**

`daemon/internal/server/harness_test.go`:

```go
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
```

- [ ] **Step 2: Write the security tests**

`daemon/internal/server/security_test.go`:

```go
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
```

- [ ] **Step 3: Write the tests for the `/command` pipeline, `/status` and `/tools`**

`daemon/internal/server/http_test.go`:

```go
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
	if !st.Running || st.Version != protocol.Version || st.ProtocolVersion != 1 || !strings.HasSuffix(addr, ":"+strconv.Itoa(st.Port)) || st.Extension.Connected {
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
```

- [ ] **Step 4: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/server/`
Expected: FAIL, `undefined: New`, `undefined: Options`, `undefined: statusBody`

- [ ] **Step 5: Write `server.go`**

`daemon/internal/server/server.go`:

```go
// Package server is the daemon's HTTP side: POST /command, GET /tools, GET /status and the /ws
// endpoint the extension connects to, behind the localhost-only security checks.
package server

import (
	"encoding/json"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/session"
)

type Options struct {
	Config       home.Config
	ArtifactsDir string
	Log          *slog.Logger
}

type Server struct {
	cfg       home.Config
	artifacts string
	log       *slog.Logger
	hub       *Hub
	queues    *session.Queues
	started   time.Time
}

func New(opt Options) *Server {
	return &Server{
		cfg:       opt.Config,
		artifacts: opt.ArtifactsDir,
		log:       opt.Log,
		hub:       NewHub(opt.Config.BlockedHosts, opt.Log),
		queues:    session.New(),
		started:   time.Now(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /command", s.handleCommand)
	mux.HandleFunc("GET /tools", s.handleTools)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /ws", s.hub.ServeWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, protocol.ErrInvalidRequest, r.Method+" "+r.URL.Path+" does not exist", "Use POST /command, GET /tools or GET /status")
	})
	return s.secure(mux)
}

// secure applies the checks of spec §7 before any handler runs.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r) {
			forbid(w, "Host must be 127.0.0.1:<port> or localhost:<port>")
			return
		}
		origin := r.Header.Get("Origin")
		if r.URL.Path == "/ws" {
			if !s.originAllowed(origin) {
				forbid(w, "only the Browser Bridge extension may connect to /ws")
				return
			}
		} else if origin != "" {
			forbid(w, "requests from web pages are not accepted")
			return
		}
		if r.Method == http.MethodPost && !isJSON(r.Header.Get("Content-Type")) {
			forbid(w, "Content-Type must be application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed defeats DNS rebinding: a page on evil.com that resolves to 127.0.0.1 still sends
// Host: evil.com. The port is the one this connection actually arrived on.
func hostAllowed(r *http.Request) bool {
	port := localPort(r)
	return port != "" && (r.Host == "127.0.0.1:"+port || r.Host == "localhost:"+port)
}

func localPort(r *http.Request) string {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return ""
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return port
}

func (s *Server) originAllowed(origin string) bool {
	for _, id := range s.cfg.ExtensionIDs {
		if origin == "chrome-extension://"+id {
			return true
		}
	}
	return false
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

func forbid(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusForbidden, protocol.ErrForbidden, msg, "")
}

func writeError(w http.ResponseWriter, status int, code, msg, hint string) {
	writeJSONBody(w, status, protocol.Fail(code, msg, hint))
}

func writeEnvelope(w http.ResponseWriter, resp protocol.Response) {
	writeJSONBody(w, protocol.HTTPStatus(resp.Error), resp)
}

func writeJSONBody(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	tools := make([]tool, 0, len(protocol.Actions))
	for _, a := range protocol.Actions {
		tools = append(tools, tool{Name: a.Name, Description: a.Description, InputSchema: protocol.InputSchema(a)})
	}
	writeJSONBody(w, http.StatusOK, tools)
}

type statusBody struct {
	Running         bool            `json:"running"`
	Version         string          `json:"version"`
	ProtocolVersion int             `json:"protocolVersion"`
	Port            int             `json:"port"`
	UptimeSeconds   int64           `json:"uptimeSeconds"`
	Extension       ExtensionStatus `json:"extension"`
	Sessions        int             `json:"sessions"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(localPort(r))
	writeJSONBody(w, http.StatusOK, statusBody{
		Running:         true,
		Version:         protocol.Version,
		ProtocolVersion: protocol.ProtocolVersion,
		Port:            port,
		UptimeSeconds:   int64(time.Since(s.started).Seconds()),
		Extension:       s.hub.Status(),
		Sessions:        s.queues.Count(),
	})
}
```

- [ ] **Step 6: Write `command.go`**

`daemon/internal/server/command.go`:

```go
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
```

- [ ] **Step 7: Write the minimal `checks.go`**

There are no checks yet; Task 8 replaces this whole file. `daemon/internal/server/checks.go`:

```go
package server

import (
	"encoding/json"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// precheck runs the checks that need no browser. Args have already passed the schema.
func (s *Server) precheck(action string, args json.RawMessage) *protocol.Error {
	return nil
}
```

- [ ] **Step 8: Run the test, confirm it passes**

Run: `go -C daemon test -count=3 ./internal/server/`
Expected: `ok`

- [ ] **Step 9: Commit**

```bash
git add daemon/internal/server
git commit -m "feat(daemon): serve /command, /tools and /status behind localhost-only checks"
```

---

### Task 8: Daemon-side checks and saving screenshots to a file

**Files:**
- Modify: `daemon/internal/server/checks.go` (replace entirely)
- Modify: `daemon/internal/server/command.go` (the `switch a.Name` block in `run`)
- Test: `daemon/internal/server/checks_test.go`

**Interfaces:**
- Consumes: `(*Server).precheck` (Task 7), `protocol.ScreenshotCapture`/`ScreenshotResult` (Task 2).
- Produces:
  - `navigate`: only `http`, `https`, `about:blank`; a URL missing its scheme returns `INVALID_REQUEST` with the hint `https://…`; other schemes and the extension store pages return `RESTRICTED_URL`; a blocked host returns `BLOCKED_HOST`
  - `find_tab`: must have `url` or `active:true`; the host is taken from `kimi.com`, `www.kimi.com/x` or a full URL
  - `blockedHosts` matching: lowercase, strip the trailing dot, strip the port, match the exact host or a subdomain
  - `upload`: every file must be an absolute path, must exist, and must not be a directory
  - `screenshot.path`: must be absolute. The extension's OK response (`ScreenshotCapture`, base64) is written to a file, and the agent receives `ScreenshotResult`. If `path` is not passed, it is written to `<artifacts>/<session>-<yyyyMMdd-HHmmss.SSS>.png|jpg`
  - `cdp`: `Browser.*` and `Target.*` return `CDP_ERROR`; reading cookies is allowed (spec §7.1)

- [ ] **Step 1: Write the failing test**

`daemon/internal/server/checks_test.go`:

```go
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
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./internal/server/ -run 'Checks|Screenshot|CDP'`
Expected: FAIL in `TestNavigateURLChecks`, `TestFindTabChecks`, `TestUploadPathChecks`, `TestCDPBlocksBrowserAndTargetDomains` and the 4 `TestScreenshot…` tests

- [ ] **Step 3: Replace the whole of `checks.go`**

`daemon/internal/server/checks.go`:

```go
package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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

// hostOf reads the host from what an agent passes to find_tab: "kimi.com", "www.kimi.com/x" or
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
	data, _ := json.Marshal(protocol.ScreenshotResult{Path: path, SizeBytes: int64(len(img)), MimeType: c.MimeType, Width: c.Width, Height: c.Height})
	return protocol.Response{OK: true, Data: data}
}
```

- [ ] **Step 4: Wire `saveScreenshot` into the pipeline**

In `daemon/internal/server/command.go`, function `run`, replace:

```go
	switch a.Name {
	case "close_session":
```

with:

```go
	switch a.Name {
	case "screenshot":
		resp = s.saveScreenshot(req.Session, req.Args, resp)
	case "close_session":
```

- [ ] **Step 5: Run all the server tests**

Run: `go -C daemon test -count=3 ./internal/server/`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/server
git commit -m "feat(daemon): check URLs, blocked hosts, file paths and CDP methods before the browser"
```

---

### Task 9: `bridge serve` and the smoke test

**Files:**
- Create: `daemon/cmd/bridge/main.go`
- Test: `daemon/cmd/bridge/main_test.go`

**Interfaces:**
- Consumes: `home` (Task 4), `server` (Task 7, 8), `protocol.Version`.
- Produces: the `bridge` binary with `serve [--addr host:port]` and `version`. The function `serve(ctx context.Context, args []string, stderr io.Writer) int` returns the exit code, so the CLI plan can call it again when it builds `start`. While running: writes `daemon.pid`/`daemon.addr` and removes them on exit via Ctrl+C; the log is written to `logs/daemon.log` and stderr.

- [ ] **Step 1: Write the failing test**

`daemon/cmd/bridge/main_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestServeWritesRuntimeFilesAndAnswersStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	var stderr bytes.Buffer
	go func() { done <- serve(ctx, []string{"--addr", addr}, &stderr) }()

	var st struct {
		Running bool `json:"running"`
		Port    int  `json:"port"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := http.Get("http://" + addr + "/status")
		if err == nil {
			json.NewDecoder(res.Body).Decode(&st)
			res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon never answered: %v\n%s", err, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !st.Running || !strings.HasSuffix(addr, ":"+strconv.Itoa(st.Port)) {
		t.Fatalf("status = %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "daemon.addr")); string(b) != addr {
		t.Fatalf("daemon.addr = %q", b)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit code %d\n%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "daemon.pid")); !os.IsNotExist(err) {
		t.Fatal("daemon.pid left behind")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "logs", "daemon.log")); !strings.Contains(string(b), "listening") {
		t.Fatalf("log = %s", b)
	}
}

func TestServeRefusesBadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"addr":"0.0.0.0:9876"}`), 0o644)
	var stderr bytes.Buffer
	if code := serve(context.Background(), nil, &stderr); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stderr.String(), "config.json") {
		t.Fatalf("stderr must name the file: %s", stderr.String())
	}
}

func TestServeRefusesBusyPort(t *testing.T) {
	t.Setenv("BRIDGE_HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var stderr bytes.Buffer
	if code := serve(context.Background(), []string{"--addr", ln.Addr().String()}, &stderr); code != 1 {
		t.Fatalf("exit code %d", code)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `go -C daemon test ./cmd/bridge/`
Expected: FAIL `[build failed]`, `undefined: serve`

- [ ] **Step 3: Write `main.go`**

`daemon/cmd/bridge/main.go`:

```go
// Command bridge is the Browser Bridge daemon and CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/server"
)

const usage = `usage: bridge <command>

  serve [--addr host:port]   run the daemon in the foreground
  version                    print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(ctx, os.Args[2:], os.Stderr))
	case "version":
		fmt.Println(protocol.Version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// serve runs the daemon until ctx ends. It returns the process exit code.
func serve(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", "", "listen address (default: addr in config.json, else "+home.DefaultAddr+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := home.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	cfg, err := home.LoadConfig(dir)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	addr, err := home.ResolveAddr(*addrFlag, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	logFile, err := home.OpenLog(dir)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(io.MultiWriter(logFile, stderr), nil))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "bridge: cannot listen on %s (is another daemon running?): %v\n", addr, err)
		return 1
	}
	if err := home.WriteRuntimeFiles(dir, os.Getpid(), ln.Addr().String()); err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	defer home.RemoveRuntimeFiles(dir)

	srv := server.New(server.Options{Config: cfg, ArtifactsDir: home.ArtifactsDir(dir), Log: log})
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", ln.Addr().String(), "version", protocol.Version, "blockedHosts", len(cfg.BlockedHosts))
	if err := httpSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve failed", "err", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run all the tests and vet**

```bash
go -C daemon vet ./...
go -C daemon test -count=1 ./...
```

Expected: `vet` prints nothing; every package is `ok` (only `internal/fakeext` reports `[no test files]`).

- [ ] **Step 5: Manual smoke test**

Terminal 1 (PowerShell):

```powershell
go -C daemon build -o bridge.exe ./cmd/bridge
$env:BRIDGE_HOME = "$env:TEMP\bridge-smoke"
daemon\bridge.exe serve --addr 127.0.0.1:9877
```

Expected: the log line `msg=listening addr=127.0.0.1:9877 version=0.1.0`.

Terminal 2:

```powershell
curl.exe -s http://127.0.0.1:9877/status
curl.exe -s -X POST http://127.0.0.1:9877/command -H "Content-Type: application/json" -d '{"action":"list_tabs","session":"smoke"}'
curl.exe -s -o NUL -w "%{http_code}\n" -X POST http://127.0.0.1:9877/command -H "Content-Type: application/json" -H "Origin: https://evil.example" -d '{}'
curl.exe -s http://127.0.0.1:9877/tools | Select-String -Pattern '"name":"navigate"' -Quiet
```

Expected, in order:
1. JSON containing `"running":true`, `"port":9877`, `"extension":{"connected":false}`.
2. `{"ok":false,"error":{"code":"EXTENSION_NOT_CONNECTED",…}}`.
3. `403`.
4. `True`.

Press Ctrl+C in terminal 1. Expected: `daemon.pid` and `daemon.addr` in `%TEMP%\bridge-smoke` have been removed; `logs\daemon.log` has a `command` line for `list_tabs`.

- [ ] **Step 6: Commit**

```bash
git add daemon/cmd/bridge
git commit -m "feat(daemon): add bridge serve"
```
