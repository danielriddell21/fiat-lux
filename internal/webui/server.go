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

type SimProvider func() *sim.Sim

type Options struct {
	Addr string

	Provider SimProvider

	Logger *log.Logger

	ImageCache *imagegen.Cache
}

type Server struct {
	provider   SimProvider
	broker     *broker
	addr       string
	srv        *http.Server
	listener   net.Listener
	logger     *log.Logger
	imageCache *imagegen.Cache
}

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

func (s *Server) Publish(res sim.StepResult) {
	s.broker.publish(res)
}

func (s *Server) Start() error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("webui: listen %s: %w", s.addr, err)
	}
	s.listener = ln
	s.logf("listening on http://%s", s.Addr())
	if !s.isLocalOnly() {
		s.logf("WARNING: web UI bound to a non-loopback address; anyone on the network can view this kosmos")
	}
	if err := s.srv.Serve(ln); err != nil {
		return fmt.Errorf("webui: serve: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	s.broker.closeAll()
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("webui: shutdown: %w", err)
	}
	return nil
}

func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

func (s *Server) assetsHandler() http.Handler {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		// Should not happen: embed.FS guarantees the prefix exists.
		s.logf("assets sub: %v", err)
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

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
