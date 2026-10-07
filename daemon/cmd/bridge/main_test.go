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

func TestServeRefusesBusyPortAndLogsWhy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var stderr bytes.Buffer
	if code := serve(context.Background(), []string{"--addr", ln.Addr().String()}, &stderr); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	// bridge start discards stderr, so the reason has to be in the log file.
	if b, _ := os.ReadFile(filepath.Join(dir, "logs", "daemon.log")); !strings.Contains(string(b), "cannot listen") {
		t.Fatalf("log = %s", b)
	}
}

func TestServeStopsOnShutdownRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_HOME", dir)
	addr := freeAddr(t)
	done := make(chan int)
	var stderr bytes.Buffer
	go func() { done <- serve(context.Background(), []string{"--addr", addr}, &stderr) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := http.Post("http://"+addr+"/shutdown", "application/json", strings.NewReader("{}"))
		if err == nil {
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatalf("shutdown answered %d", res.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon never answered: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d\n%s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after /shutdown")
	}
	if _, err := os.Stat(filepath.Join(dir, "daemon.addr")); !os.IsNotExist(err) {
		t.Fatal("daemon.addr left behind")
	}
}

func TestHelpIsOutputNotAnError(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{arg}, &stdout, &stderr); code != 0 {
			t.Errorf("%s: exit code %d", arg, code)
		}
		if !strings.Contains(stdout.String(), "usage: bridge") || stderr.Len() != 0 {
			t.Errorf("%s: usage must go to stdout; stdout=%q stderr=%q", arg, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"bogus"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: bridge") {
		t.Errorf("an unknown command is still an error: exit %d, stderr %q", code, stderr.String())
	}
	if code := run(context.Background(), nil, &stdout, &stderr); code != 2 {
		t.Errorf("no command: exit %d", code)
	}
}
