package server

import (
	"slices"
	"sync"

	"github.com/gorilla/websocket"

	"element-agent/internal/protocol"
)

type conn struct {
	ws     *websocket.Conn
	send   chan protocol.Frame
	agents []string
	closed chan struct{}
	once   sync.Once
}

func newConn(ws *websocket.Conn) *conn {
	return &conn{
		ws:     ws,
		send:   make(chan protocol.Frame, 16),
		closed: make(chan struct{}),
	}
}

func (c *conn) push(frame protocol.Frame) bool {
	select {
	case c.send <- frame:
		return true
	case <-c.closed:
		return false
	}
}

func (c *conn) shutdown() {
	c.once.Do(func() {
		close(c.closed)
		c.ws.Close()
	})
}

type job struct {
	id      string
	agent   string
	roomID  string
	eventID string
	owner   *conn
}

type hub struct {
	mu     sync.Mutex
	agents map[string]*conn
	jobs   map[string]*job
}

func newHub() *hub {
	return &hub{agents: map[string]*conn{}, jobs: map[string]*job{}}
}

func (h *hub) serve(c *conn, agents []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, name := range c.agents {
		if h.agents[name] == c {
			delete(h.agents, name)
		}
	}
	c.agents = slices.Clone(agents)
	for _, name := range agents {
		h.agents[name] = c
	}
}

func (h *hub) drop(c *conn) []*job {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, name := range c.agents {
		if h.agents[name] == c {
			delete(h.agents, name)
		}
	}
	var orphaned []*job
	for id, j := range h.jobs {
		if j.owner == c {
			orphaned = append(orphaned, j)
			delete(h.jobs, id)
		}
	}
	return orphaned
}

func (h *hub) connFor(agent string) *conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agents[agent]
}

func (h *hub) addJob(j *job) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobs[j.id] = j
}

func (h *hub) job(id string, owner *conn) (*job, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, ok := h.jobs[id]
	if !ok || j.owner != owner {
		return nil, false
	}
	return j, true
}

func (h *hub) takeJob(id string, owner *conn) (*job, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, ok := h.jobs[id]
	if !ok || j.owner != owner {
		return nil, false
	}
	delete(h.jobs, id)
	return j, true
}
