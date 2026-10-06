package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// bridgeBin is the real binary: start re-runs its own executable with serve, which a test binary
// cannot stand in for.
var bridgeBin string

func TestMain(m *testing.M) {
	if os.Getenv("BRIDGE_TEST_HELPER") == "sleep" {
		// A long-running process that is not bridge, for TestStopRefusesAProcessThatIsNotBridge.
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "bridge-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bridgeBin = filepath.Join(dir, "bridge")
	if runtime.GOOS == "windows" {
		bridgeBin += ".exe"
	}
	build := exec.Command(goTool(), "build", "-o", bridgeBin, ".")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building bridge:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func goTool() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

// newHome is a BRIDGE_HOME whose config puts the daemon on a free port, so tests never meet a
// daemon the developer is running on 9876. Any daemon left running is stopped at the end.
func newHome(t *testing.T) (dir, addr string) {
	t.Helper()
	dir = t.TempDir()
	addr = freeAddr(t)
	cfg, _ := json.Marshal(map[string]string{"addr": addr})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runBridge(t, dir, "stop") })
	return dir, addr
}

func runBridge(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bridgeBin, args...)
	cmd.Env = append(os.Environ(), "BRIDGE_HOME="+dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		return out.String(), exitErr.ExitCode()
	case err != nil:
		t.Fatalf("bridge %v: %v", args, err)
	}
	return out.String(), 0
}

func statusPID(t *testing.T, dir string) int {
	t.Helper()
	out, code := runBridge(t, dir, "status")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, out)
	}
	var st struct {
		Running bool `json:"running"`
		PID     int  `json:"pid"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil || !st.Running || st.PID == 0 {
		t.Fatalf("status output %q", out)
	}
	return st.PID
}

func TestStatusWhenNotRunning(t *testing.T) {
	dir, addr := newHome(t)
	out, code := runBridge(t, dir, "status")
	if code != 1 || strings.TrimSpace(out) != `{"running":false,"addr":"`+addr+`"}` {
		t.Fatalf("exit %d, output %q", code, out)
	}
}

func TestStartStatusStop(t *testing.T) {
	dir, addr := newHome(t)
	out, code := runBridge(t, dir, "start")
	if code != 0 || !strings.Contains(out, "bridge started at "+addr) {
		t.Fatalf("start: exit %d, %q", code, out)
	}
	pid := statusPID(t, dir)

	out, code = runBridge(t, dir, "start")
	if code != 0 || !strings.Contains(out, "already running") {
		t.Fatalf("second start: exit %d, %q", code, out)
	}
	if again := statusPID(t, dir); again != pid {
		t.Fatalf("second start replaced the daemon: pid %d, then %d", pid, again)
	}

	out, code = runBridge(t, dir, "stop")
	if code != 0 || strings.TrimSpace(out) != "bridge stopped" {
		t.Fatalf("stop: exit %d, %q", code, out)
	}
	if _, code := runBridge(t, dir, "status"); code != 1 {
		t.Fatal("status after stop must exit 1")
	}
	for _, f := range []string{"daemon.pid", "daemon.addr"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s left behind", f)
		}
	}
	if p, err := openProcess(pid); err == nil {
		exited := p.wait(time.Second)
		p.close()
		if !exited {
			t.Fatalf("daemon pid %d still running after stop", pid)
		}
	}
}

func TestStopWhenNotRunningRemovesStaleFiles(t *testing.T) {
	dir, addr := newHome(t)
	os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte("999999"), 0o644)
	os.WriteFile(filepath.Join(dir, "daemon.addr"), []byte(addr), 0o644)
	out, code := runBridge(t, dir, "stop")
	if code != 0 || strings.TrimSpace(out) != "bridge is not running" {
		t.Fatalf("exit %d, %q", code, out)
	}
	for _, f := range []string{"daemon.pid", "daemon.addr"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("stale %s not removed", f)
		}
	}
}

func TestStartShowsWhyTheDaemonFailed(t *testing.T) {
	dir, addr := newHome(t)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	out, code := runBridge(t, dir, "start")
	if code != 1 || !strings.Contains(out, "cannot listen") {
		t.Fatalf("exit %d, %q", code, out)
	}
}

func TestRestartReplacesTheDaemon(t *testing.T) {
	dir, _ := newHome(t)
	if out, code := runBridge(t, dir, "start"); code != 0 {
		t.Fatalf("start: %s", out)
	}
	before := statusPID(t, dir)
	if out, code := runBridge(t, dir, "restart"); code != 0 {
		t.Fatalf("restart: exit %d, %q", code, out)
	}
	if after := statusPID(t, dir); after == before {
		t.Fatal("restart kept the same daemon")
	}
}

// startChild starts cmd and returns a channel closed once it exits. The child is killed at the
// end of the test.
func startChild(t *testing.T, cmd *exec.Cmd) <-chan struct{} {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	go func() {
		cmd.Wait()
		close(gone)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-gone
	})
	return gone
}

// fakeDaemon answers on dir's daemon.addr like a hung daemon: /status names pid until that process
// exits, and /shutdown answers 200 without doing anything.
func fakeDaemon(t *testing.T, dir string, pid int, gone <-chan struct{}) {
	t.Helper()
	fake := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gone:
			http.Error(w, "gone", http.StatusServiceUnavailable)
			return
		default:
		}
		if r.URL.Path == "/status" {
			fmt.Fprintf(w, `{"running":true,"pid":%d}`, pid)
		}
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go fake.Serve(ln)
	t.Cleanup(func() { fake.Close() })
	os.WriteFile(filepath.Join(dir, "daemon.addr"), []byte(ln.Addr().String()), 0o644)
}

// A daemon that answers /status but never exits after /shutdown must be killed, and only the
// process /status names.
func TestStopKillsADaemonThatIgnoresShutdown(t *testing.T) {
	dir, _ := newHome(t)
	stuck := exec.Command(bridgeBin, "serve", "--addr", freeAddr(t))
	stuck.Env = append(os.Environ(), "BRIDGE_HOME="+t.TempDir())
	gone := startChild(t, stuck)
	fakeDaemon(t, dir, stuck.Process.Pid, gone)

	start := time.Now()
	out, code := runBridge(t, dir, "stop")
	if code != 0 || !strings.Contains(out, fmt.Sprintf("killed pid %d", stuck.Process.Pid)) {
		t.Fatalf("exit %d, %q", code, out)
	}
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("the stuck daemon is still running")
	}
	if took := time.Since(start); took < stopTimeout {
		t.Fatalf("stop killed after %v, before giving the daemon %v to exit", took, stopTimeout)
	}
}

// Whatever answers on the daemon's address could name any pid; stop must not touch a process
// that does not run bridge.
func TestStopRefusesAProcessThatIsNotBridge(t *testing.T) {
	dir, _ := newHome(t)
	other := exec.Command(os.Args[0])
	other.Env = append(os.Environ(), "BRIDGE_TEST_HELPER=sleep")
	gone := startChild(t, other)
	fakeDaemon(t, dir, other.Process.Pid, gone)

	out, code := runBridge(t, dir, "stop")
	if code != 1 || !strings.Contains(out, "not stopping it") {
		t.Fatalf("exit %d, %q", code, out)
	}
	select {
	case <-gone:
		t.Fatal("stop killed a process that does not run bridge")
	case <-time.After(500 * time.Millisecond):
	}
}
