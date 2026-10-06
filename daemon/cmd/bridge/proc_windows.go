//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// DETACHED_PROCESS: the daemon gets no console and does not belong to the terminal that ran
// bridge start, so closing that terminal does not stop it.
const detachedProcess = 0x00000008

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

// daemonProcess is a handle on the daemon opened while its pid is known to be current. Windows
// does not reuse a pid while a handle to it is open, so wait and kill always target the daemon.
// os.FindProcess is not used because its handle lacks PROCESS_TERMINATE.
type daemonProcess struct{ h syscall.Handle }

func openProcess(pid int) (*daemonProcess, error) {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return &daemonProcess{h}, nil
}

// wait reports whether the process exited within d.
func (p *daemonProcess) wait(d time.Duration) bool {
	ev, err := syscall.WaitForSingleObject(p.h, uint32(d.Milliseconds()))
	return err == nil && ev == syscall.WAIT_OBJECT_0
}

func (p *daemonProcess) kill() error { return syscall.TerminateProcess(p.h, 1) }

func (p *daemonProcess) close() { syscall.CloseHandle(p.h) }
