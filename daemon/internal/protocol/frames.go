package protocol

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// WebSocket frames between daemon and extension. Every frame is a JSON text message whose "type"
// field names the frame.

type Hello struct {
	Type             string `json:"type" jsonschema:"enum=hello"`
	ProtocolVersion  int    `json:"protocolVersion"`
	ExtensionVersion string `json:"extensionVersion"`
	ExtensionID      string `json:"extensionId"`
	Browser          string `json:"browser" jsonschema:"enum=chrome,enum=edge"`
	// Actions lets GET /tools tell an agent which actions this extension can run, so it does not
	// have to find out by calling them.
	Actions []string `json:"actions"`
}

// JSONSchemaExtend pins protocolVersion to the Go constant. The extension's generated type then
// becomes the literal version, so bumping it here breaks the extension's build until it follows.
func (Hello) JSONSchemaExtend(s *jsonschema.Schema) { pinProtocolVersion(s) }

func (Welcome) JSONSchemaExtend(s *jsonschema.Schema) { pinProtocolVersion(s) }

func pinProtocolVersion(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("protocolVersion"); ok {
		p.Const = ProtocolVersion
	}
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
