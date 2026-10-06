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
