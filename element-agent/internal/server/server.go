package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"element-agent/internal/matrix"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 54 * time.Second
	maxFrameBytes  = 4 << 20
	txnCacheLimit  = 1024
	shutdownGrace  = 5 * time.Second
	dispatchBudget = 30 * time.Second
)

var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Server struct {
	cfg   Config
	mx    *matrix.Client
	state *State
	hub   *hub
	mux   *http.ServeMux
	seen  *txnCache
	up    websocket.Upgrader
}

func New(cfg Config) (*Server, error) {
	state, err := LoadState(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:   cfg,
		mx:    matrix.New(cfg.Homeserver, cfg.ServerName, cfg.ASToken, cfg.AdminToken),
		state: state,
		hub:   newHub(),
		mux:   http.NewServeMux(),
		seen:  newTxnCache(txnCacheLimit),
		up:    websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096},
	}, nil
}

func (s *Server) Setup() {
	s.mux.HandleFunc("PUT /_matrix/app/v1/transactions/{txnId}", s.homeserverAuth(s.handleTransaction))
	s.mux.HandleFunc("GET /_matrix/app/v1/users/{userId}", s.homeserverAuth(s.handleQueryUser))

	prefix := s.cfg.ClientPrefix
	s.mux.HandleFunc("POST "+prefix+"/reserve", s.clientAuth(s.handleReserve))
	s.mux.HandleFunc("POST "+prefix+"/register", s.clientAuth(s.handleRegister))
	s.mux.HandleFunc("POST "+prefix+"/deregister", s.clientAuth(s.handleDeregister))
	s.mux.HandleFunc("GET "+prefix+"/ws", s.clientAuth(s.handleWebSocket))
}

func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Addr: s.cfg.Listen, Handler: s.mux}

	go s.reconcileLoop(ctx)

	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		srv.Shutdown(stop)
	}()

	log.Info().Str("addr", s.cfg.Listen).Str("server_name", s.cfg.ServerName).Msg("listening")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) homeserverAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bearer(r) != s.cfg.HSToken {
			writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "bad homeserver token")
			return
		}
		next(w, r)
	}
}

func (s *Server) clientAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bearer(r) != s.cfg.SharedSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

type txnCache struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	order []string
	limit int
}

func newTxnCache(limit int) *txnCache {
	return &txnCache{seen: map[string]struct{}{}, limit: limit}
}

func (t *txnCache) add(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.seen[id]; ok {
		return false
	}
	t.seen[id] = struct{}{}
	t.order = append(t.order, id)
	if len(t.order) > t.limit {
		delete(t.seen, t.order[0])
		t.order = t.order[1:]
	}
	return true
}
