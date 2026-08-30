package server

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"element-agent/internal/matrix"
)

func (s *Server) reconcileLoop(ctx context.Context) {
	s.reconcileAll(ctx)
	ticker := time.NewTicker(s.cfg.ReconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileAll(ctx)
		}
	}
}

func (s *Server) reconcileAll(ctx context.Context) {
	agents := s.state.Names()
	if len(agents) == 0 {
		return
	}
	spaces, err := s.mx.Spaces(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to list spaces")
		return
	}
	for _, name := range agents {
		s.reconcileAgent(ctx, name, spaces)
	}
}

func (s *Server) reconcileAgent(ctx context.Context, name string, spaces []matrix.AdminRoom) {
	userID := s.mx.UserID(name)
	for _, space := range spaces {
		if err := s.mx.AdminJoin(ctx, space.RoomID, userID); err != nil {
			log.Warn().Err(err).Str("agent", name).Str("space", space.RoomID).Msg("failed to join space")
			continue
		}
		children, err := s.mx.Hierarchy(ctx, space.RoomID, userID)
		if err != nil {
			log.Warn().Err(err).Str("agent", name).Str("space", space.RoomID).Msg("failed to read hierarchy")
			continue
		}
		for _, child := range children {
			if child.RoomID == space.RoomID {
				continue
			}
			if err := s.mx.Join(ctx, child.RoomID, userID); err != nil {
				log.Warn().Err(err).Str("agent", name).Str("room", child.RoomID).Msg("failed to join channel")
			}
		}
	}
}

func (s *Server) reconcileOne(ctx context.Context, name string) {
	spaces, err := s.mx.Spaces(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to list spaces")
		return
	}
	s.reconcileAgent(ctx, name, spaces)
}
