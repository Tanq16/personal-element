package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"encoding/json/v2"

	"element-agent/internal/protocol"
)

type agentRequest struct {
	Name    string `json:"name"`
	Claim   string `json:"claim"`
	Release bool   `json:"release"`
}

func newClaim() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (s *Server) readRequest(w http.ResponseWriter, r *http.Request) (agentRequest, bool) {
	var req agentRequest
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		http.Error(w, "malformed body", http.StatusBadRequest)
		return req, false
	}
	if !agentNamePattern.MatchString(req.Name) {
		http.Error(w, "name must match ^[a-z0-9][a-z0-9_-]{0,31}$", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

func (s *Server) authorize(w http.ResponseWriter, req agentRequest) bool {
	record, ok := s.state.Lookup(req.Name)
	if !ok {
		http.Error(w, "no such agent, reserve it first", http.StatusNotFound)
		return false
	}
	if record.Claim != req.Claim {
		http.Error(w, "this machine does not hold that agent", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleReserve(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	if _, taken := s.state.Lookup(req.Name); taken {
		http.Error(w, "name already reserved", http.StatusConflict)
		return
	}

	claim, err := newClaim()
	if err != nil {
		log.Error().Err(err).Str("agent", req.Name).Msg("failed to mint a claim")
		http.Error(w, "could not mint a claim", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()

	if err := s.mx.RegisterAgent(ctx, req.Name); err != nil {
		log.Error().Err(err).Str("agent", req.Name).Msg("failed to create agent user")
		http.Error(w, "could not create the agent account", http.StatusBadGateway)
		return
	}
	if err := s.state.Reserve(req.Name, claim); err != nil {
		log.Error().Err(err).Str("agent", req.Name).Msg("failed to persist state")
		http.Error(w, "could not persist the reservation", http.StatusInternalServerError)
		return
	}
	s.reconcileOne(ctx, req.Name)

	log.Info().Str("agent", req.Name).Msg("reserved")
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": s.mx.UserID(req.Name), "claim": claim})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok || !s.authorize(w, req) {
		return
	}
	if err := s.state.SetServing(req.Name, true); err != nil {
		log.Error().Err(err).Str("agent", req.Name).Msg("failed to persist state")
		http.Error(w, "could not persist the registration", http.StatusInternalServerError)
		return
	}
	log.Info().Str("agent", req.Name).Msg("registered")
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleDeregister(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok || !s.authorize(w, req) {
		return
	}

	err := s.state.SetServing(req.Name, false)
	event := "deregistered"
	if req.Release {
		err = s.state.Release(req.Name)
		event = "released"
	}
	if err != nil {
		log.Error().Err(err).Str("agent", req.Name).Msg("failed to persist state")
		http.Error(w, "could not persist the change", http.StatusInternalServerError)
		return
	}
	log.Info().Str("agent", req.Name).Msg(event)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		log.Warn().Err(err).Msg("failed to upgrade")
		return
	}
	c := newConn(ws)
	go s.writePump(c)
	s.readPump(c)
}

func (s *Server) readPump(c *conn) {
	defer func() {
		orphaned := s.hub.drop(c)
		c.shutdown()
		for _, j := range orphaned {
			go s.onOrphan(j)
		}
	}()

	c.ws.SetReadLimit(maxFrameBytes)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var frame protocol.Frame
		if err := json.Unmarshal(data, &frame); err != nil {
			log.Warn().Err(err).Msg("malformed frame")
			continue
		}
		s.handleFrame(c, frame)
	}
}

func (s *Server) handleFrame(c *conn, frame protocol.Frame) {
	switch frame.Type {
	case protocol.TypeHello:
		s.onHello(c, frame)
	case protocol.TypeResult:
		j, ok := s.hub.takeJob(frame.JobID, c)
		if !ok {
			log.Warn().Str("job", frame.JobID).Msg("result for an unknown job")
			return
		}
		go s.onResult(j, frame)
	case protocol.TypeContextRequest:
		j, ok := s.hub.job(frame.JobID, c)
		if !ok {
			log.Warn().Str("job", frame.JobID).Msg("context request for an unknown job")
			return
		}
		go s.onContextRequest(j, frame)
	default:
		log.Warn().Str("type", frame.Type).Msg("unknown frame")
	}
}

func (s *Server) onHello(c *conn, frame protocol.Frame) {
	var accepted, refused []string
	for _, claim := range frame.Claims {
		record, ok := s.state.Lookup(claim.Name)
		if !ok || record.Claim != claim.Claim {
			refused = append(refused, claim.Name)
			continue
		}
		accepted = append(accepted, claim.Name)
	}
	s.hub.serve(c, accepted)

	log.Info().Strs("agents", accepted).Msg("client connected")
	if len(refused) == 0 {
		return
	}
	log.Warn().Strs("agents", refused).Msg("refused agents with an unrecognised claim")
	c.push(protocol.Frame{
		Type:    protocol.TypeError,
		Message: "refused, this machine does not hold the claim: " + strings.Join(refused, ", "),
	})
}

func (s *Server) writePump(c *conn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.shutdown()
	}()

	for {
		select {
		case <-c.closed:
			return
		case frame := <-c.send:
			data, err := json.Marshal(frame)
			if err != nil {
				log.Error().Err(err).Msg("failed to encode frame")
				continue
			}
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
