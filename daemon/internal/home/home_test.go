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
	if got := RuntimeAddr(dir); got != "127.0.0.1:9876" {
		t.Fatalf("RuntimeAddr = %q", got)
	}
	RemoveRuntimeFiles(dir)
	if got := RuntimeAddr(dir); got != "" {
		t.Fatalf("RuntimeAddr after removal = %q", got)
	}
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
