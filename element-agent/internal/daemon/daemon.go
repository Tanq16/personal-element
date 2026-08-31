package daemon

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"encoding/json/v2"

	"element-agent/internal/protocol"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 54 * time.Second
	maxFrameBytes  = 4 << 20
	minBackoff     = time.Second
	maxBackoff     = 30 * time.Second
	contextTimeout = 60 * time.Second
	queueDepth     = 8
)

type Config struct {
	ServerURL string
	Secret    string
	Root      string
	Loopback  string
	Timeout   time.Duration
}

type Daemon struct {
	cfg      Config
	store    *Store
	loopback *loopback

	out chan protocol.Frame
	ctx context.Context
	wg  sync.WaitGroup

	mu      sync.Mutex
	agents  map[string]Agent
	queues  map[string]chan protocol.Frame
	waiters map[string]chan protocol.Frame
}

func New(cfg Config) (*Daemon, error) {
	store := NewStore(cfg.Root)
	agents, err := store.List()
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, errors.New("no agents are configured, run 'element-agent client init' first")
	}

	d := &Daemon{
		cfg:     cfg,
		store:   store,
		out:     make(chan protocol.Frame, 32),
		agents:  map[string]Agent{},
		queues:  map[string]chan protocol.Frame{},
		waiters: map[string]chan protocol.Frame{},
	}
	for _, agent := range agents {
		d.agents[agent.Name] = agent
	}

	lb, err := newLoopback(cfg.Loopback, d.requestContext, d.reload)
	if err != nil {
		return nil, err
	}
	d.loopback = lb
	return d, nil
}

func (d *Daemon) Run(ctx context.Context) error {
	d.ctx = ctx

	d.wg.Go(func() {
		if err := d.loopback.serve(ctx); err != nil {
			log.Error().Err(err).Msg("loopback stopped")
		}
	})

	d.connectLoop(ctx)
	d.wg.Wait()
	return nil
}

func (d *Daemon) reload() error {
	agents, err := d.store.List()
	if err != nil {
		return err
	}

	d.mu.Lock()
	for _, agent := range agents {
		d.agents[agent.Name] = agent
	}
	d.mu.Unlock()

	log.Info().Int("agents", len(agents)).Msg("reloaded the agent directory")
	d.send(d.hello())
	return nil
}

func (d *Daemon) hello() protocol.Frame {
	d.mu.Lock()
	defer d.mu.Unlock()

	claims := make([]protocol.Claim, 0, len(d.agents))
	for _, name := range slices.Sorted(maps.Keys(d.agents)) {
		claims = append(claims, protocol.Claim{Name: name, Claim: d.agents[name].Claim})
	}
	return protocol.Frame{Type: protocol.TypeHello, Claims: claims}
}

func (d *Daemon) connectLoop(ctx context.Context) {
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := d.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("connection lost")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (d *Daemon) session(ctx context.Context) error {
	target, err := websocketURL(d.cfg.ServerURL)
	if err != nil {
		return err
	}

	header := http.Header{"Authorization": {"Bearer " + d.cfg.Secret}}
	ws, resp, err := websocket.DefaultDialer.DialContext(ctx, target, header)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial %s: %s", target, resp.Status)
		}
		return err
	}
	defer ws.Close()

	hello := d.hello()
	if err := writeFrame(ws, hello); err != nil {
		return err
	}
	names := make([]string, 0, len(hello.Claims))
	for _, claim := range hello.Claims {
		names = append(names, claim.Name)
	}
	log.Info().Strs("agents", names).Str("server", d.cfg.ServerURL).Msg("connected")

	session, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-session.Done()
		ws.Close()
	}()

	go d.writePump(session, ws)
	return d.readPump(ws)
}

func (d *Daemon) readPump(ws *websocket.Conn) error {
	ws.SetReadLimit(maxFrameBytes)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		var frame protocol.Frame
		if err := json.Unmarshal(data, &frame); err != nil {
			log.Warn().Err(err).Msg("malformed frame")
			continue
		}

		switch {
		case frame.Type == protocol.TypeJob:
			d.enqueue(frame)
		case frame.Type == protocol.TypeError && frame.JobID == "":
			log.Warn().Msg(frame.Message)
		case frame.Type == protocol.TypeContextResponse, frame.Type == protocol.TypeError:
			d.deliver(frame)
		default:
			log.Warn().Str("type", frame.Type).Msg("unknown frame")
		}
	}
}

func (d *Daemon) writePump(ctx context.Context, ws *websocket.Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-d.out:
			if err := writeFrame(ws, frame); err != nil {
				return
			}
		case <-ticker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (d *Daemon) enqueue(frame protocol.Frame) {
	queue, ok := d.laneFor(frame.Agent)
	if !ok {
		d.send(protocol.Frame{
			Type:  protocol.TypeResult,
			JobID: frame.JobID,
			Error: "this machine does not serve " + frame.Agent,
		})
		return
	}
	select {
	case queue <- frame:
	default:
		d.send(protocol.Frame{
			Type:  protocol.TypeResult,
			JobID: frame.JobID,
			Error: frame.Agent + " has too many jobs queued",
		})
	}
}

func (d *Daemon) laneFor(name string) (chan protocol.Frame, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, known := d.agents[name]; !known {
		return nil, false
	}
	if queue, running := d.queues[name]; running {
		return queue, true
	}

	queue := make(chan protocol.Frame, queueDepth)
	d.queues[name] = queue
	d.wg.Go(func() { d.work(name, queue) })
	return queue, true
}

func (d *Daemon) work(name string, queue <-chan protocol.Frame) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case frame := <-queue:
			d.send(d.execute(name, frame))
		}
	}
}

func (d *Daemon) execute(name string, frame protocol.Frame) protocol.Frame {
	d.mu.Lock()
	agent := d.agents[name]
	d.mu.Unlock()

	token, err := d.loopback.issue(frame.JobID)
	if err != nil {
		return protocol.Frame{Type: protocol.TypeResult, JobID: frame.JobID, Error: err.Error()}
	}
	defer d.loopback.release(token)

	log.Info().Str("agent", name).Str("job", frame.JobID).Msg("running")
	dir := d.store.Dir(name)
	output, err := Run(d.ctx, Job{
		Agent:   agent,
		Dir:     dir,
		Prompt:  composePrompt(agent, dir, frame.Body, frame.Limit, d.cfg.Loopback),
		Token:   token,
		Timeout: d.cfg.Timeout,
	})
	if err != nil {
		log.Error().Err(err).Str("agent", name).Str("job", frame.JobID).Msg("job failed")
		return protocol.Frame{Type: protocol.TypeResult, JobID: frame.JobID, Error: err.Error()}
	}

	log.Info().Str("agent", name).Str("job", frame.JobID).Msg("job finished")
	return protocol.Frame{Type: protocol.TypeResult, JobID: frame.JobID, OK: true, Output: output}
}

func (d *Daemon) requestContext(jobID, from string) (string, string, error) {
	waiter := make(chan protocol.Frame, 1)

	d.mu.Lock()
	d.waiters[jobID] = waiter
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.waiters, jobID)
		d.mu.Unlock()
	}()

	d.send(protocol.Frame{Type: protocol.TypeContextRequest, JobID: jobID, From: from})

	select {
	case frame := <-waiter:
		if frame.Type == protocol.TypeError {
			return "", "", errors.New(frame.Message)
		}
		return frame.Transcript, frame.Next, nil
	case <-time.After(contextTimeout):
		return "", "", errors.New("the server did not answer in time")
	}
}

func (d *Daemon) deliver(frame protocol.Frame) {
	d.mu.Lock()
	waiter, ok := d.waiters[frame.JobID]
	d.mu.Unlock()
	if !ok {
		return
	}
	select {
	case waiter <- frame:
	default:
	}
}

func (d *Daemon) send(frame protocol.Frame) {
	select {
	case d.out <- frame:
	default:
		log.Warn().Str("job", frame.JobID).Msg("dropped a frame, the send queue is full")
	}
}

func writeFrame(ws *websocket.Conn, frame protocol.Frame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	ws.SetWriteDeadline(time.Now().Add(writeWait))
	return ws.WriteMessage(websocket.TextMessage, data)
}

func websocketURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https", "wss":
		parsed.Scheme = "wss"
	case "http", "ws":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/ws"
	return parsed.String(), nil
}
