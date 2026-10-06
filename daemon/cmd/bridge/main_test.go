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

func TestServeRefusesBusyPort(t *testing.T) {
	t.Setenv("BRIDGE_HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var stderr bytes.Buffer
	if code := serve(context.Background(), []string{"--addr", ln.Addr().String()}, &stderr); code != 1 {
		t.Fatalf("exit code %d", code)
	}
}

