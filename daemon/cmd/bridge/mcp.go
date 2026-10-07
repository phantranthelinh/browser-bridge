package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/client"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/mcp"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
)

func mcpCmd(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	e, ok := loadHome(stderr)
	if !ok {
		return 1
	}
	link := &daemonLink{env: e, log: stderr}
	link.ensureRunning()
	server := mcp.NewServer(mcp.NewSession(), link.send)
	err := server.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}})
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, "bridge mcp:", err)
		return 1
	}
	return 0
}

// daemonLink reaches the daemon for bridge mcp and starts it when it is not running: an MCP client
// launches bridge mcp by itself, with nobody around to run bridge start first.
type daemonLink struct {
	env homeEnv
	log io.Writer // stdout carries the MCP messages, so what start prints goes here
	mu  sync.Mutex
}

// ensureRunning starts the daemon unless one answers, and reports whether it started one.
func (d *daemonLink) ensureRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if addr, err := d.env.daemonAddr(); err == nil {
		if _, _, err := fetchStatus(addr); err == nil {
			return false
		}
	}
	return startCmd(nil, d.log, d.log) == 0
}

func (d *daemonLink) send(ctx context.Context, req protocol.Request) (protocol.Response, error) {
	resp, err := d.command(ctx, req)
	var unreachable *client.UnreachableError
	// Started only when nothing answered, so the command cannot have run already.
	if errors.As(err, &unreachable) && d.ensureRunning() {
		return d.command(ctx, req)
	}
	return resp, err
}

func (d *daemonLink) command(ctx context.Context, req protocol.Request) (protocol.Response, error) {
	addr, err := d.env.daemonAddr()
	if err != nil {
		return protocol.Response{}, err
	}
	resp, _, err := client.New(addr).Command(ctx, req)
	return resp, err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
