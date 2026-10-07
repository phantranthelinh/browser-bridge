// Package server is the daemon's HTTP side: POST /command, GET /tools, GET /status and the /ws
// endpoint the extension connects to, behind the localhost-only security checks.
package server

import (
	"encoding/json"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/protocol"
	"github.com/phantranthelinh/browser-bridge/daemon/internal/session"
)

type Options struct {
	Config       home.Config
	ArtifactsDir string
	Log          *slog.Logger
	// OnShutdown is called once when POST /shutdown arrives, after the reply is written.
	OnShutdown func()
}

type Server struct {
	cfg        home.Config
	artifacts  string
	log        *slog.Logger
	hub        *Hub
	queues     *session.Queues
	started    time.Time
	onShutdown func()
	shutdown   sync.Once
}

func New(opt Options) *Server {
	return &Server{
		cfg:        opt.Config,
		artifacts:  opt.ArtifactsDir,
		log:        opt.Log,
		hub:        NewHub(opt.Config.BlockedHosts, opt.Log),
		queues:     session.New(),
		started:    time.Now(),
		onShutdown: opt.OnShutdown,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /command", s.handleCommand)
	mux.HandleFunc("GET /tools", s.handleTools)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /ws", s.hub.ServeWS)
	mux.HandleFunc("POST /shutdown", s.handleShutdown)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, protocol.ErrInvalidRequest, r.Method+" "+r.URL.Path+" does not exist", "Use POST /command, GET /tools or GET /status")
	})
	return s.secure(mux)
}

// secure applies the checks of spec §7 before any handler runs.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r) {
			forbid(w, "Host must be 127.0.0.1:<port> or localhost:<port>")
			return
		}
		origin := r.Header.Get("Origin")
		if r.URL.Path == "/ws" {
			if !s.originAllowed(origin) {
				forbid(w, "only the Browser Bridge extension may connect to /ws")
				return
			}
		} else if origin != "" {
			forbid(w, "requests from web pages are not accepted")
			return
		}
		if r.Method == http.MethodPost && !isJSON(r.Header.Get("Content-Type")) {
			forbid(w, "Content-Type must be application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed defeats DNS rebinding: a page on evil.com that resolves to 127.0.0.1 still sends
// Host: evil.com. The port is the one this connection actually arrived on.
func hostAllowed(r *http.Request) bool {
	port := localPort(r)
	return port != "" && (r.Host == "127.0.0.1:"+port || r.Host == "localhost:"+port)
}

func localPort(r *http.Request) string {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return ""
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return port
}

func (s *Server) originAllowed(origin string) bool {
	for _, id := range s.cfg.ExtensionIDs {
		if origin == "chrome-extension://"+id {
			return true
		}
	}
	return false
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

func forbid(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusForbidden, protocol.ErrForbidden, msg, "")
}

func writeError(w http.ResponseWriter, status int, code, msg, hint string) {
	writeJSONBody(w, status, protocol.Fail(code, msg, hint))
}

func writeEnvelope(w http.ResponseWriter, resp protocol.Response) {
	writeJSONBody(w, protocol.HTTPStatus(resp.Error), resp)
}

func writeJSONBody(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// Available is false for an action the connected extension does not implement, and for every
	// action while no extension is connected.
	Available bool `json:"available"`
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	tools := make([]tool, 0, len(protocol.Actions))
	for _, a := range protocol.Actions {
		tools = append(tools, tool{Name: a.Name, Description: a.Description, InputSchema: protocol.InputSchema(a), Available: s.hub.Implements(a.Name)})
	}
	writeJSONBody(w, http.StatusOK, tools)
}

type statusBody struct {
	Running         bool            `json:"running"`
	Version         string          `json:"version"`
	ProtocolVersion int             `json:"protocolVersion"`
	Port            int             `json:"port"`
	PID             int             `json:"pid"`
	UptimeSeconds   int64           `json:"uptimeSeconds"`
	Extension       ExtensionStatus `json:"extension"`
	Sessions        int             `json:"sessions"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(localPort(r))
	writeJSONBody(w, http.StatusOK, statusBody{
		Running:         true,
		Version:         protocol.Version,
		ProtocolVersion: protocol.ProtocolVersion,
		Port:            port,
		PID:             os.Getpid(),
		UptimeSeconds:   int64(time.Since(s.started).Seconds()),
		Extension:       s.hub.Status(),
		Sessions:        s.queues.Count(),
	})
}

// handleShutdown replies before stopping, so bridge stop gets a clean 200 rather than a dropped
// connection.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeEnvelope(w, protocol.Response{OK: true, Data: json.RawMessage("{}")})
	s.shutdown.Do(func() {
		s.log.Info("shutdown requested")
		if s.onShutdown != nil {
			s.onShutdown()
		}
	})
}
