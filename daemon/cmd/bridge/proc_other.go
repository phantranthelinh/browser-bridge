//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Only Windows is supported (spec §1.3); this keeps the package building elsewhere.

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

type daemonProcess struct{ p *os.Process }

func openProcess(pid int) (*daemonProcess, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	if p.Signal(syscall.Signal(0)) != nil {
		return nil, errors.New("no such process")
	}
	return &daemonProcess{p}, nil
}

func (p *daemonProcess) wait(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for p.p.Signal(syscall.Signal(0)) == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

func (p *daemonProcess) kill() error { return p.p.Kill() }

func (p *daemonProcess) close() {}
