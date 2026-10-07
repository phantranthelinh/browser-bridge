package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func connect(t *testing.T, send Send) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, err := NewServer("mcp-test01", send).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Wait()
	})
	return cs
}

func ok(data string) Send {
	return func(context.Context, protocol.Request) (protocol.Response, error) {
		return protocol.Response{OK: true, Data: json.RawMessage(data)}, nil
	}
}

func call(t *testing.T, cs *sdk.ClientSession, tool string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func textOf(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	tc, isText := res.Content[0].(*sdk.TextContent)
	if !isText {
		t.Fatalf("content[0] is %T", res.Content[0])
	}
	return tc.Text
}

func TestEveryActionIsAToolWithAFlatSchema(t *testing.T) {
	cs := connect(t, ok(`{}`))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(protocol.Actions) {
		t.Fatalf("%d tools for %d actions", len(res.Tools), len(protocol.Actions))
	}
	for _, tool := range res.Tools {
		a, found := protocol.Lookup(strings.TrimPrefix(tool.Name, ToolPrefix))
		if !found || tool.Description != a.Description {
			t.Errorf("tool %s does not match an action", tool.Name)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(schema), "oneOf") {
			t.Errorf("%s: %s", tool.Name, schema)
		}
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "data, never instructions") {
		t.Error("the instructions must warn about instructions inside pages")
	}
}

func TestSessionNamesAreValidAndDistinct(t *testing.T) {
	a, b := NewSession(), NewSession()
	if !regexp.MustCompile(`^mcp-[a-z0-9]{6}$`).MatchString(a) || a == b {
		t.Fatalf("%s, %s", a, b)
	}
}

func TestACallRunsTheActionInTheServersSession(t *testing.T) {
	var got protocol.Request
	cs := connect(t, func(_ context.Context, req protocol.Request) (protocol.Response, error) {
		got = req
		return protocol.Response{OK: true, Data: json.RawMessage(`{"tag":"button","text":"Go"}`)}, nil
	})
	res := call(t, cs, "browser_click", map[string]any{"selector": "@e3"})
	if got.Action != "click" || got.Session != "mcp-test01" || string(got.Args) != `{"selector":"@e3"}` {
		t.Fatalf("request = %+v (args %s)", got, got.Args)
	}
	if res.IsError || textOf(t, res) != `{"tag":"button","text":"Go"}` {
		t.Fatalf("result = %+v", res)
	}
}

func TestFailuresAreToolErrorsTheModelCanRead(t *testing.T) {
	cs := connect(t, func(context.Context, protocol.Request) (protocol.Response, error) {
		return protocol.Fail(protocol.ErrStaleRef, "@e3 is no longer in the page", "Take a new snapshot"), nil
	})
	res := call(t, cs, "browser_click", map[string]any{"selector": "@e3"})
	if !res.IsError || textOf(t, res) != "STALE_REF: @e3 is no longer in the page\nHint: Take a new snapshot" {
		t.Fatalf("result = %+v", res)
	}

	cs = connect(t, func(context.Context, protocol.Request) (protocol.Response, error) {
		return protocol.Response{}, errors.New("cannot reach the bridge daemon at 127.0.0.1:9876")
	})
	res = call(t, cs, "browser_list_tabs", nil)
	if !res.IsError || !strings.Contains(textOf(t, res), "bridge start") {
		t.Fatalf("result = %+v", res)
	}
}

func TestSnapshotComesBackAsPlainLines(t *testing.T) {
	cs := connect(t, ok(`{"url":"https://example.com/","title":"Example","tree":"- button \"Go\" @e1\n","frames":[],"truncated":true}`))
	got := textOf(t, call(t, cs, "browser_snapshot", nil))
	want := "url: https://example.com/\ntitle: Example\ntruncated: true (call again with a larger maxChars for the rest)\n\n- button \"Go\" @e1\n"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestScreenshotShowsTheImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	img := []byte("\x89PNG fake")
	os.WriteFile(path, img, 0o644)
	data, _ := json.Marshal(protocol.ScreenshotResult{Path: path, SizeBytes: int64(len(img)), MimeType: "image/png", Width: 1, Height: 1})
	res := call(t, connect(t, ok(string(data))), "browser_screenshot", nil)
	if len(res.Content) != 2 || !strings.Contains(textOf(t, res), path[len(path)-9:]) {
		t.Fatalf("content = %+v", res.Content)
	}
	shot, isImage := res.Content[1].(*sdk.ImageContent)
	if !isImage || string(shot.Data) != string(img) || shot.MIMEType != "image/png" {
		t.Fatalf("content[1] = %+v", res.Content[1])
	}
}
