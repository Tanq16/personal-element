package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

const LoopbackAddr = "127.0.0.1:5167"

var ErrDaemonRunning = errors.New("another daemon already holds the loopback port")

type pager func(jobID, from string) (transcript, next string, err error)

type reloader func() error

type localJob struct {
	id        string
	next      string
	exhausted bool
}

type loopback struct {
	addr     string
	listener net.Listener
	ask      pager
	refresh  reloader

	mu   sync.Mutex
	jobs map[string]*localJob
}

func newLoopback(addr string, ask pager, refresh reloader) (*loopback, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			return nil, ErrDaemonRunning
		}
		return nil, err
	}
	return &loopback{addr: addr, listener: listener, ask: ask, refresh: refresh, jobs: map[string]*localJob{}}, nil
}

func (l *loopback) issue(jobID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.jobs[token] = &localJob{id: jobID}
	return token, nil
}

func (l *loopback) release(token string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.jobs, token)
}

func (l *loopback) serve(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /context", l.localOnly(l.handleContext))
	mux.HandleFunc("POST /reload", l.localOnly(l.handleReload))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		srv.Shutdown(stop)
	}()

	log.Info().Str("addr", l.addr).Msg("loopback listening")
	if err := srv.Serve(l.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (l *loopback) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Host != l.addr {
			http.Error(w, "unexpected host", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (l *loopback) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := l.refresh(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (l *loopback) handleContext(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	l.mu.Lock()
	job, known := l.jobs[token]
	var id, from string
	exhausted := true
	if known {
		id, from, exhausted = job.id, job.next, job.exhausted
	}
	l.mu.Unlock()

	if !known {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if exhausted {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("(no earlier messages)\n"))
		return
	}

	transcript, next, err := l.ask(id, from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	l.mu.Lock()
	if current, still := l.jobs[token]; still {
		current.next = next
		current.exhausted = next == ""
	}
	l.mu.Unlock()

	if transcript == "" {
		transcript = "(no earlier messages)"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(transcript + "\n"))
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
