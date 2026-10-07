package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
)

const (
	startTimeout = 5 * time.Second
	stopTimeout  = 5 * time.Second
	killTimeout  = 2 * time.Second
)

// homeEnv is the home folder and its config, which every lifecycle command reads first.
type homeEnv struct {
	dir string
	cfg home.Config
}

func loadHome(stderr io.Writer) (homeEnv, bool) {
	dir, err := home.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return homeEnv{}, false
	}
	cfg, err := home.LoadConfig(dir)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return homeEnv{}, false
	}
	return homeEnv{dir, cfg}, true
}

// daemonAddr is where to look for the daemon: the address a running daemon wrote to
// daemon.addr, otherwise the configured one.
func (e homeEnv) daemonAddr() (string, error) {
	if a := home.RuntimeAddr(e.dir); a != "" {
		return a, nil
	}
	return home.ResolveAddr("", e.cfg)
}

func startCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", "", "listen address (default: addr in config.json, else "+home.DefaultAddr+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	e, ok := loadHome(stderr)
	if !ok {
		return 1
	}
	if a := home.RuntimeAddr(e.dir); a != "" {
		if st, _, err := fetchStatus(a); err == nil {
			fmt.Fprintf(stdout, "bridge is already running at %s (pid %d)\n", a, st.PID)
			return 0
		}
	}
	addr, err := home.ResolveAddr(*addrFlag, e.cfg)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	// No Stdin/Stdout/Stderr: the daemon must not hold the caller's pipes open, or a script that
	// runs bridge start (install.ps1) would wait for the daemon to exit.
	cmd := exec.Command(exe, "serve", "--addr", addr)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stderr, "bridge: cannot start the daemon:", err)
		return 1
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		// The pid check rules out another daemon that already held this port.
		if st, _, err := fetchStatus(addr); err == nil && st.PID == cmd.Process.Pid {
			fmt.Fprintf(stdout, "bridge started at %s (pid %d)\n", addr, st.PID)
			return 0
		}
		select {
		case <-exited:
			return startFailed(e.dir, stderr, "the daemon exited right away")
		case <-time.After(100 * time.Millisecond):
		}
	}
	cmd.Process.Kill()
	return startFailed(e.dir, stderr, fmt.Sprintf("the daemon did not answer within %s", startTimeout))
}

func startFailed(dir string, stderr io.Writer, reason string) int {
	logPath := home.LogPath(dir)
	fmt.Fprintf(stderr, "bridge: %s. Last lines of %s:\n", reason, logPath)
	for _, line := range tail(logPath, 10) {
		fmt.Fprintln(stderr, "  "+line)
	}
	return 1
}

func tail(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{"(no log: " + err.Error() + ")"}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func stopCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	e, ok := loadHome(stderr)
	if !ok {
		return 1
	}
	addr, err := e.daemonAddr()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	st, _, err := fetchStatus(addr)
	if err != nil {
		// pid and addr files of a daemon that was killed without cleaning up after itself.
		home.RemoveRuntimeFiles(e.dir)
		fmt.Fprintln(stdout, "bridge is not running")
		return 0
	}
	// The pid comes from the live daemon's /status, never from daemon.pid: that file may be stale
	// and its number already given to an unrelated process.
	proc, err := openProcess(st.PID)
	if err != nil {
		fmt.Fprintf(stderr, "bridge: cannot open the daemon process (pid %d): %v\n", st.PID, err)
		return 1
	}
	defer proc.close()
	if err := checkIsBridge(proc, st.PID); err != nil {
		fmt.Fprintf(stderr, "bridge: %v; not stopping it\n", err)
		return 1
	}
	if err := requestShutdown(addr); err != nil {
		fmt.Fprintln(stderr, "bridge: shutdown request failed, killing the daemon:", err)
	} else if proc.wait(stopTimeout) {
		home.RemoveRuntimeFiles(e.dir)
		fmt.Fprintln(stdout, "bridge stopped")
		return 0
	}
	if err := proc.kill(); err == nil && proc.wait(killTimeout) {
		home.RemoveRuntimeFiles(e.dir)
		fmt.Fprintf(stdout, "bridge stopped (killed pid %d)\n", st.PID)
		return 0
	}
	fmt.Fprintf(stderr, "bridge: could not stop the daemon (pid %d)\n", st.PID)
	return 1
}

// checkIsBridge guards the kill in stopCmd. Whatever answers on the daemon's address names the
// pid, and nothing proves that answer came from bridge; without this, a bridge stop run from an
// admin shell could be steered into killing any process. Only a process running an executable
// with our own name is touched.
func checkIsBridge(p *daemonProcess, pid int) error {
	path, err := p.imagePath()
	if err != nil {
		return fmt.Errorf("cannot read the executable of pid %d: %v", pid, err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(path), filepath.Base(self)) {
		return fmt.Errorf("pid %d named by /status runs %s, not %s", pid, path, filepath.Base(self))
	}
	return nil
}

func restartCmd(args []string, stdout, stderr io.Writer) int {
	if code := stopCmd(nil, stdout, stderr); code != 0 {
		return code
	}
	return startCmd(args, stdout, stderr)
}

func statusCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	e, ok := loadHome(stderr)
	if !ok {
		return 1
	}
	addr, err := e.daemonAddr()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	_, body, err := fetchStatus(addr)
	if err != nil {
		json.NewEncoder(stdout).Encode(struct {
			Running bool   `json:"running"`
			Addr    string `json:"addr"`
		}{false, addr})
		return 1
	}
	stdout.Write(body)
	return 0
}
