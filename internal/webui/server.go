// Package webui hosts an embedded HTTP viewer for the active sim.
// The server exposes a JSON snapshot of the focused world plus a
// Server-Sent Events stream that pushes a notification on every
// Sim.Step. Static assets are baked in via go:embed so the binary
// stays single-file.
package webui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/imagegen"
	"github.com/danielriddell21/fiat-lux/internal/sim"
)

// SimProvider returns the sim whose world should be rendered. The
// caller is responsible for keeping it in sync with TUI focus: for a
// single sim it can be `func() *sim.Sim { return s }`; for a
// Universe it can be `func() *sim.Sim { return u.Focused() }`.
type SimProvider func() *sim.Sim

// Options configures a new Server.
type Options struct {
	// Addr is the listen address, e.g. ":8080" or "127.0.0.1:8080".
	// Required.
	Addr string

	// Provider returns the currently-focused sim on every request.
	// Required.
	Provider SimProvider

	// Logger receives lifecycle messages (start, shutdown, errors).
	// Nil disables logging.
	Logger *log.Logger

	// ImageCache, when non-nil, makes /api/image/<hash>.png serve
	// the bytes the multimodal generator wrote.
	ImageCache *imagegen.Cache
}

// Server is the HTTP front door. Construct with New, attach
// Publish to one or more Sim.Observer fields, then call Start to
// begin listening; Shutdown cleans up.
type Server struct {
	provider   SimProvider
	broker     *broker
	addr       string
	srv        *http.Server
	listener   net.Listener
	logger     *log.Logger
	imageCache *imagegen.Cache
}

// New constructs a Server. The HTTP listener is not opened until
// Start is called.
func New(opts Options) (*Server, error) {
	if opts.Provider == nil {
		return nil, errors.New("webui: Options.Provider is required")
	}
	if opts.Addr == "" {
		return nil, errors.New("webui: Options.Addr is required")
	}
	s := &Server{
		provider:   opts.Provider,
		broker:     newBroker(),
		addr:       opts.Addr,
		logger:     opts.Logger,
		imageCache: opts.ImageCache,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/intervene", s.handleIntervene)
	mux.HandleFunc("/api/image/", s.handleImage)
	mux.HandleFunc("/api/annals", s.handleAnnals)
	mux.Handle("/assets/", http.StripPrefix("/assets/", s.assetsHandler()))
	mux.HandleFunc("/", s.handleIndex)
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

// Publish is the callback to attach to sim.Sim.Observer. It is safe
// for concurrent calls.
func (s *Server) Publish(res sim.StepResult) {
	s.broker.publish(res)
}

// Start opens the listener and serves in the current goroutine.
// Returns http.ErrServerClosed on a clean shutdown.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("webui: listen %s: %w", s.addr, err)
	}
	s.listener = ln
	s.logf("listening on http://%s", s.Addr())
	if !s.isLocalOnly() {
		s.logf("WARNING: web UI bound to a non-loopback address; anyone on the network can view this kosmos")
	}
	return s.srv.Serve(ln)
}

// Shutdown gracefully closes the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	s.broker.closeAll()
	return s.srv.Shutdown(ctx)
}

// Addr returns the resolved listen address; useful after Start
// when the configured Addr was ":0" (random port).
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

// assetsHandler serves the embedded static files under /assets/.
func (s *Server) assetsHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		// Should not happen: embed.FS guarantees the prefix exists.
		s.logf("assets sub: %v", err)
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

// handleIndex serves the single-page UI. Falls back to 404 for
// anything other than the root.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(assetsFS, "assets/index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) logf(format string, args ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Printf("webui: "+format, args...)
}

// isLocalOnly reports whether the configured address is loopback
// only. Both an empty host and "localhost"/"127.0.0.1"/"::1" count.
func (s *Server) isLocalOnly() bool {
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		host = s.addr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false // ":8080" listens on all interfaces
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}
