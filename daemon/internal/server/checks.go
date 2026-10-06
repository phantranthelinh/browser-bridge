package server

import (
	"encoding/json"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

// precheck runs the checks that need no browser. Args have already passed the schema.
func (s *Server) precheck(action string, args json.RawMessage) *protocol.Error {
	return nil
}
