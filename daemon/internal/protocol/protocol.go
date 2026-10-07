// Package protocol is the single definition of the bridge wire format: the HTTP envelope, the
// argument and result types of every action, and the WebSocket frames between daemon and
// extension. The JSON Schema, the extension's TypeScript types, GET /tools and the MCP tool list
// are all generated from it, so a field is added here and nowhere else.
package protocol

import "encoding/json"

const (
	Version         = "0.2.0"
	ProtocolVersion = 2
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
