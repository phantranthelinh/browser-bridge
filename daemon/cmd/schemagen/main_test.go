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

// The skill's action table is what agents read to call actions, so it must follow the schema.
func TestCommittedSkillTableIsUpToDate(t *testing.T) {
	got, err := os.ReadFile("../../skill/browser-bridge/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	want, err := protocol.WithToolTable(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the action table in daemon/skill/browser-bridge/SKILL.md is stale (run: go -C daemon run ./cmd/schemagen)")
	}
}
