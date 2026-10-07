// Command bridge is the Browser Bridge daemon and CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/server"
)

const usage = `usage: bridge <command>

  call <action> --session <name> [key=value ...]
                               run one action and print the result (bridge call -h for more)
  start [--addr host:port]     run the daemon in the background
  stop                         stop the daemon
  restart [--addr host:port]   stop, then start
  status                       print the daemon status as JSON (exit 1 when not running)
  logs [-f] [-n N] [--prev]    print the end of the daemon log
  mcp                          serve the actions as MCP tools over stdio
  install-skill [--remove]     add the browser-bridge skill to Claude Code and Codex
  serve [--addr host:port]     run the daemon in the foreground
  version                      print the version
  help                         print this help
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches one subcommand and returns the process exit code.
func run(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	args := argv[1:]
	switch argv[0] {
	case "call":
		return callCmd(ctx, args, stdin, stdout, stderr)
	case "mcp":
		return mcpCmd(ctx, args, stdin, stdout, stderr)
	case "logs":
		return logsCmd(ctx, args, stdout, stderr)
	case "install-skill":
		return installSkillCmd(args, stdout, stderr)
	case "start":
		return startCmd(args, stdout, stderr)
	case "stop":
		return stopCmd(args, stdout, stderr)
	case "restart":
		return restartCmd(args, stdout, stderr)
	case "status":
		return statusCmd(args, stdout, stderr)
	case "serve":
		return serve(ctx, args, stderr)
	case "version":
		fmt.Fprintln(stdout, protocol.Version)
		return 0
	case "help", "-h", "--help", "-help":
		// Asked for, so it is the output rather than an error.
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// serve runs the daemon until ctx ends. It returns the process exit code.
func serve(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addrFlag := fs.String("addr", "", "listen address (default: addr in config.json, else "+home.DefaultAddr+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := home.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	cfg, err := home.LoadConfig(dir)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	addr, err := home.ResolveAddr(*addrFlag, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	logFile, err := home.OpenLog(dir)
	if err != nil {
		fmt.Fprintln(stderr, "bridge:", err)
		return 1
	}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(io.MultiWriter(logFile, stderr), nil))

	// From here on errors go to the log too: under bridge start stderr is discarded, and start
	// shows the end of the log when the daemon fails to come up.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("cannot listen (is another daemon running?)", "addr", addr, "err", err)
		return 1
	}
	if err := home.WriteRuntimeFiles(dir, os.Getpid(), ln.Addr().String()); err != nil {
		log.Error("cannot write runtime files", "err", err)
		return 1
	}
	defer home.RemoveRuntimeFiles(dir)

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	srv := server.New(server.Options{Config: cfg, ArtifactsDir: home.ArtifactsDir(dir), Log: log, OnShutdown: stop})
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", ln.Addr().String(), "version", protocol.Version, "blockedHosts", len(cfg.BlockedHosts))
	if err := httpSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve failed", "err", err)
		return 1
	}
	log.Info("stopped")
	return 0
}
