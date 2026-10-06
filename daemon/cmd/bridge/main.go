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

  serve [--addr host:port]   run the daemon in the foreground
  version                    print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch os.Args[1] {
	case "serve":
		os.Exit(serve(ctx, os.Args[2:], os.Stderr))
	case "version":
		fmt.Println(protocol.Version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
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
