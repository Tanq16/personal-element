package server

import (
	"context"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/rs/zerolog/log"

	"element-agent/internal/matrix"
	"element-agent/internal/protocol"
)

func (s *Server) handleEvent(ctx context.Context, event matrix.Event) {
	if !event.IsMessage() || event.Content.Body == "" {
		return
	}
	for _, name := range s.state.Serving() {
		userID := s.mx.UserID(name)
		if event.MentionsUser(userID) {
			s.dispatch(ctx, event, name, userID)
		}
	}
}

func (s *Server) dispatch(ctx context.Context, event matrix.Event, agent, userID string) {
	owner := s.hub.connFor(agent)
	if owner == nil {
		s.notify(ctx, event.RoomID, userID, fmt.Sprintf("agent_%s is not connected right now.", agent))
		return
	}

	j := &job{
		id:      uuid.New().String(),
		agent:   agent,
		roomID:  event.RoomID,
		eventID: event.EventID,
		owner:   owner,
	}
	s.hub.addJob(j)

	frame := protocol.Frame{
		Type:    protocol.TypeJob,
		JobID:   j.id,
		Agent:   agent,
		RoomID:  event.RoomID,
		EventID: event.EventID,
		Body:    event.Content.Body,
		Limit:   s.cfg.BackfillLimit,
	}
	if !owner.push(frame) {
		s.hub.takeJob(j.id, owner)
		s.notify(ctx, event.RoomID, userID, fmt.Sprintf("agent_%s went offline before the job started.", agent))
		return
	}
	log.Info().Str("agent", agent).Str("job", j.id).Str("room", event.RoomID).Msg("dispatched")
}

func (s *Server) onResult(j *job, frame protocol.Frame) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	userID := s.mx.UserID(j.agent)
	body := frame.Output
	if !frame.OK {
		body = fmt.Sprintf("agent_%s failed: %s", j.agent, strings.TrimSpace(frame.Error))
	}

	chunks := matrix.Chunk(strings.TrimSpace(body))
	if len(chunks) == 0 {
		chunks = []string{fmt.Sprintf("agent_%s returned nothing.", j.agent)}
	}
	for _, chunk := range chunks {
		if _, err := s.mx.Send(ctx, j.roomID, userID, chunk); err != nil {
			log.Error().Err(err).Str("agent", j.agent).Str("job", j.id).Msg("failed to post reply")
			return
		}
	}
	log.Info().Str("agent", j.agent).Str("job", j.id).Int("chunks", len(chunks)).Msg("replied")
}

func (s *Server) onOrphan(j *job) {
	ctx, cancel := context.WithTimeout(context.Background(), dispatchBudget)
	defer cancel()

	log.Warn().Str("agent", j.agent).Str("job", j.id).Msg("client disconnected mid-job")
	s.notify(ctx, j.roomID, s.mx.UserID(j.agent),
		fmt.Sprintf("agent_%s disconnected before answering.", j.agent))
}

func (s *Server) onContextRequest(j *job, frame protocol.Frame) {
	ctx, cancel := context.WithTimeout(context.Background(), dispatchBudget)
	defer cancel()

	userID := s.mx.UserID(j.agent)
	history, next, err := s.page(ctx, j, userID, frame.From)
	if err != nil {
		log.Error().Err(err).Str("agent", j.agent).Str("job", j.id).Msg("failed to page history")
		j.owner.push(protocol.Frame{Type: protocol.TypeError, JobID: j.id, Message: "could not read older history"})
		return
	}
	if len(history) == 0 {
		next = ""
	}
	j.owner.push(protocol.Frame{
		Type:       protocol.TypeContextResponse,
		JobID:      j.id,
		Transcript: renderTranscript(history),
		Next:       next,
	})
}

func (s *Server) page(ctx context.Context, j *job, userID, from string) ([]matrix.Message, string, error) {
	if from == "" {
		return s.mx.Backfill(ctx, j.roomID, j.eventID, userID, s.cfg.BackfillLimit)
	}
	return s.mx.Older(ctx, j.roomID, userID, from, s.cfg.BackfillLimit)
}

func (s *Server) notify(ctx context.Context, roomID, userID, message string) {
	if _, err := s.mx.Send(ctx, roomID, userID, message); err != nil {
		log.Error().Err(err).Str("room", roomID).Msg("failed to post notice")
	}
}

func renderTranscript(history []matrix.Message) string {
	if len(history) == 0 {
		return ""
	}
	var out strings.Builder
	for _, msg := range history {
		fmt.Fprintf(&out, "%s: %s\n", msg.Sender, msg.Body)
	}
	return strings.TrimRight(out.String(), "\n")
}
