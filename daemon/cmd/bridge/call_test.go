package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/fakeext"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/server"
)

func TestBuildArgsTypesValuesFromTheSchema(t *testing.T) {
	cases := []struct {
		action string
		base   string
		pairs  []string
		want   string
	}{
		{"navigate", "", []string{"url=https://example.com/?a=b", "newTab=true"}, `{"newTab":true,"url":"https://example.com/?a=b"}`},
		{"scroll", "", []string{"direction=down", "amount=300"}, `{"amount":300,"direction":"down"}`},
		{"upload", "", []string{"selector=#f", `files=C:\a.pdf`, `files=C:\b.pdf`}, `{"files":["C:\\a.pdf","C:\\b.pdf"],"selector":"#f"}`},
		{"cdp", "", []string{"method=DOM.getDocument", `params:={"depth":1}`}, `{"method":"DOM.getDocument","params":{"depth":1}}`},
		{"fill", `{"selector":"#a","value":"x"}`, []string{"selector=#b"}, `{"selector":"#b","value":"x"}`},
		{"evaluate", "", []string{"code=1 === 1"}, `{"code":"1 === 1"}`},
		// Not what the schema wants: sent as given, for the daemon to explain.
		{"navigate", "", []string{"url=x", "newTab=yes"}, `{"newTab":"yes","url":"x"}`},
		{"no_such_action", "", []string{"n=1"}, `{"n":"1"}`},
		{"list_tabs", "", nil, ``},
		{"list_tabs", `{}`, nil, `{}`},
	}
	for _, c := range cases {
		got, err := buildArgs(c.action, []byte(c.base), c.pairs)
		if err != nil || string(got) != c.want {
			t.Errorf("%s %v: got %s, %v; want %s", c.action, c.pairs, got, err, c.want)
		}
	}
	for _, bad := range [][]string{{"novalue"}, {"=x"}, {"depth:=notjson"}} {
		if _, err := buildArgs("cdp", nil, bad); err == nil {
			t.Errorf("%v must fail", bad)
		}
	}
	if _, err := buildArgs("cdp", []byte(`[1]`), nil); err == nil {
		t.Error("--json that is not an object must fail")
	}
}

// daemonAt runs a daemon in the test process with a fake extension, and points BRIDGE_HOME at it.
func daemonAt(t *testing.T, handler fakeext.Handler) {
	t.Helper()
	srv := server.New(server.Options{
		Config:       home.Config{ExtensionIDs: []string{protocol.DefaultExtensionID}},
		ArtifactsDir: t.TempDir(),
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ext, err := fakeext.Dial(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", fakeext.Options{Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ext.Close)
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	if err := home.WriteRuntimeFiles(dir, os.Getpid(), strings.TrimPrefix(ts.URL, "http://")); err != nil {
		t.Fatal(err)
	}
}

func runCall(stdin string, args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = run(context.Background(), append([]string{"call"}, args...), strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestCallPrintsTheEnvelopeAndExitsByOutcome(t *testing.T) {
	daemonAt(t, func(f protocol.RequestFrame) (protocol.Response, bool) {
		if f.Action == "click" {
			return protocol.Fail(protocol.ErrStaleRef, "@e9 is no longer in the page", "Take a new snapshot"), true
		}
		return fakeext.Echo(f)
	})

	out, _, code := runCall("", "fill", "--session", "s1", "selector=@e1", "value=Phở bò")
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Action string          `json:"action"`
			Args   json.RawMessage `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || !env.OK || env.Data.Action != "fill" || string(env.Data.Args) != `{"selector":"@e1","value":"Phở bò"}` {
		t.Fatalf("exit %d, output %q", code, out)
	}
	if !strings.HasSuffix(out, "}\n") {
		t.Errorf("output must end with one newline: %q", out)
	}

	// Flags after the arguments, and the arguments from stdin.
	out, _, code = runCall(`{"selector":"@e2","value":"x"}`, "fill", "--json-file", "-", "--session", "s1")
	if code != 0 || !strings.Contains(out, `"selector":"@e2"`) {
		t.Fatalf("exit %d, output %q", code, out)
	}

	out, _, code = runCall("", "click", "--session", "s1", "selector=@e9")
	if code != 1 || !strings.Contains(out, `"code":"STALE_REF"`) {
		t.Fatalf("a failed action: exit %d, output %q", code, out)
	}
	out, _, code = runCall("", "evaluate", "--session", "s1", "expression=1")
	if code != 1 || !strings.Contains(out, `"code":"INVALID_REQUEST"`) || !strings.Contains(out, "missing property 'code'") {
		t.Fatalf("args the daemon rejects: exit %d, output %q", code, out)
	}
}

func TestCallUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"click"}, {"click", "--session", "s1", "nokey"}, {"click", "--session", "s1", "--json", "{}", "--json-file", "x"}} {
		if _, stderr, code := runCall("", args...); code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	if out, _, code := runCall("", "-h"); code != 0 || !strings.Contains(out, "usage: bridge call") {
		t.Errorf("-h: exit %d, %q", code, out)
	}
}

func TestCallWithoutADaemonSaysHowToStartIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	cfg, _ := json.Marshal(map[string]string{"addr": freeAddr(t)})
	os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o644)
	out, stderr, code := runCall("", "list_tabs", "--session", "s1")
	if code != 2 || out != "" || !strings.Contains(stderr, "bridge start") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}
