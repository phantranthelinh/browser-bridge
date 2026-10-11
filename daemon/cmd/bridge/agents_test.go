package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/skill"
)

func TestMCPStartsTheDaemonAndServesEveryAction(t *testing.T) {
	dir, _ := newHome(t)
	cmd := exec.Command(bridgeBin, "mcp")
	cmd.Env = append(os.Environ(), "BRIDGE_HOME="+dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != len(protocol.Actions) {
		t.Fatalf("tools: %v, %v", tools, err)
	}
	// No extension connects in this test: the call reaches the daemon bridge mcp started, which
	// answers for the missing extension.
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "browser_list_tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].(*sdk.TextContent).Text; !res.IsError || !strings.HasPrefix(text, "EXTENSION_NOT_CONNECTED") {
		t.Fatalf("result: %v %q", res.IsError, text)
	}
	statusPID(t, dir)
}

// isolateAgents points every agent folder into the test's temp dir, Antigravity's through the
// home directory since it has no variable of its own. Nothing is installed until a test creates it.
func isolateAgents(t *testing.T) (claude, codex, antigravity string) {
	t.Helper()
	user := t.TempDir()
	t.Setenv("USERPROFILE", user)
	t.Setenv("HOME", user)
	claude = filepath.Join(user, "claude")
	codex = filepath.Join(user, "no-codex")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	return claude, codex, filepath.Join(user, ".gemini", "config")
}

func TestInstallSkillWritesOnlyForInstalledAgents(t *testing.T) {
	claude, codex, antigravity := isolateAgents(t)
	os.MkdirAll(claude, 0o755)

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"install-skill"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	installed, err := os.ReadFile(filepath.Join(claude, "skills", "browser-bridge", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	embedded, _ := fs.ReadFile(skill.FS, "browser-bridge/SKILL.md")
	if !bytes.Equal(installed, embedded) {
		t.Error("the installed skill differs from the one in the binary")
	}
	if _, err := os.Stat(codex); !os.IsNotExist(err) {
		t.Error("install-skill created a folder for an agent that is not installed")
	}
	if _, err := os.Stat(antigravity); !os.IsNotExist(err) {
		t.Error("install-skill created a folder for Antigravity, which is not installed")
	}
	for _, want := range []string{"Codex: not installed", "Antigravity: not installed", "[mcp_servers.browser-bridge]", "mcp_config.json", "claude mcp add browser-bridge"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := run(context.Background(), []string{"install-skill", "--remove"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("remove: exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(claude, "skills", "browser-bridge")); !os.IsNotExist(err) {
		t.Error("--remove left the skill behind")
	}
}

func TestInstallSkillAgentPicksWhichAgentsGetIt(t *testing.T) {
	claude, codex, antigravity := isolateAgents(t)
	for _, dir := range []string{claude, codex, antigravity} {
		os.MkdirAll(dir, 0o755)
	}
	skillIn := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "skills", "browser-bridge", "SKILL.md"))
		return err == nil
	}

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"install-skill", "--agent", "Antigravity, claude"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !skillIn(antigravity) || !skillIn(claude) || skillIn(codex) {
		t.Fatalf("skill in antigravity %v, claude %v, codex %v; want only the first two", skillIn(antigravity), skillIn(claude), skillIn(codex))
	}

	out.Reset()
	if code := run(context.Background(), []string{"install-skill", "--list"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("--list: exit %d", code)
	}
	for _, want := range []string{"claude\tClaude Code\tskill\t", "codex\tCodex\tinstalled\t", "antigravity\tAntigravity\tskill\t"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--list lacks %q:\n%s", want, out.String())
		}
	}

	if code := run(context.Background(), []string{"install-skill", "--remove", "--agent", "claude"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("remove: exit %d", code)
	}
	if skillIn(claude) || !skillIn(antigravity) {
		t.Error("--remove --agent claude should remove only Claude Code's skill")
	}

	errOut.Reset()
	if code := run(context.Background(), []string{"install-skill", "--agent", "cursor"}, nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "unknown agent") {
		t.Errorf("an unknown agent: exit %d, %s", code, errOut.String())
	}
	os.RemoveAll(codex)
	errOut.Reset()
	if code := run(context.Background(), []string{"install-skill", "--agent", "codex"}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "Codex: not installed") {
		t.Errorf("asking for an agent that is not installed: exit %d, %s", code, errOut.String())
	}
}

// syncBuffer is written by logs -f while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogsPrintsTheEndAndFollows(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	log := filepath.Join(dir, "logs", "daemon.log")
	os.MkdirAll(filepath.Dir(log), 0o755)
	os.WriteFile(log, []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(log+".prev", []byte("old\n"), 0o644)

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"logs", "-n", "2"}, nil, &out, &errOut); code != 0 || out.String() != "two\nthree\n" {
		t.Fatalf("exit %d, %q %s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"logs", "--prev"}, nil, &out, &errOut); code != 0 || out.String() != "old\n" {
		t.Fatalf("--prev: exit %d, %q", code, out.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	followed := &syncBuffer{}
	done := make(chan int)
	go func() { done <- run(ctx, []string{"logs", "-f", "-n", "1"}, nil, followed, &errOut) }()
	waitFor(t, func() bool { return followed.String() == "three\n" })
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("four\n")
	f.Close()
	waitFor(t, func() bool { return followed.String() == "three\nfour\n" })
	// A restart starts the log over.
	os.WriteFile(log, []byte("new\n"), 0o644)
	waitFor(t, func() bool { return followed.String() == "three\nfour\nnew\n" })
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("-f exit %d", code)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
