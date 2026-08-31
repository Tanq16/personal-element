package matrix

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"uuid"
)

const messageFilter = `{"types":["m.room.message"]}`

type Message struct {
	Sender string
	Body   string
}

type registerRequest struct {
	Type         string `json:"type"`
	Username     string `json:"username"`
	InhibitLogin bool   `json:"inhibit_login"`
}

func (c *Client) RegisterAgent(ctx context.Context, name string) error {
	body := registerRequest{
		Type:         "m.login.application_service",
		Username:     "agent_" + name,
		InhibitLogin: true,
	}
	err := c.do(ctx, "POST", "/_matrix/client/v3/register", c.asToken, nil, body, nil)
	if ErrCode(err) == "M_USER_IN_USE" {
		return nil
	}
	return err
}

func (c *Client) Send(ctx context.Context, roomID, userID, body string) (string, error) {
	var out struct {
		EventID string `json:"event_id"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/send/m.room.message/" + uuid.New().String()
	content := messageContent(body)
	if err := c.do(ctx, "PUT", path, c.asToken, asUser(userID), content, &out); err != nil {
		return "", err
	}
	return out.EventID, nil
}

func (c *Client) Join(ctx context.Context, roomID, userID string) error {
	path := "/_matrix/client/v3/join/" + url.PathEscape(roomID)
	return c.do(ctx, "POST", path, c.asToken, asUser(userID), struct{}{}, nil)
}

func (c *Client) Backfill(ctx context.Context, roomID, eventID, userID string, limit int) ([]Message, string, error) {
	var out struct {
		Start        string  `json:"start"`
		EventsBefore []Event `json:"events_before"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/context/" + url.PathEscape(eventID)
	query := asUser(userID)
	query.Set("limit", strconv.Itoa(limit))
	query.Set("filter", messageFilter)
	if err := c.do(ctx, "GET", path, c.asToken, query, nil, &out); err != nil {
		return nil, "", err
	}
	return toMessages(out.EventsBefore), out.Start, nil
}

func (c *Client) Older(ctx context.Context, roomID, userID, from string, limit int) ([]Message, string, error) {
	var out struct {
		Chunk []Event `json:"chunk"`
		End   string  `json:"end"`
	}
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/messages"
	query := asUser(userID)
	query.Set("dir", "b")
	query.Set("limit", strconv.Itoa(limit))
	query.Set("filter", messageFilter)
	if from != "" {
		query.Set("from", from)
	}
	if err := c.do(ctx, "GET", path, c.asToken, query, nil, &out); err != nil {
		return nil, "", err
	}
	return toMessages(out.Chunk), out.End, nil
}

type HierarchyRoom struct {
	RoomID   string `json:"room_id"`
	RoomType string `json:"room_type"`
}

func (c *Client) Hierarchy(ctx context.Context, spaceID, userID string) ([]HierarchyRoom, error) {
	var all []HierarchyRoom
	from := ""
	for {
		var out struct {
			Rooms     []HierarchyRoom `json:"rooms"`
			NextBatch string          `json:"next_batch"`
		}
		path := "/_matrix/client/v1/rooms/" + url.PathEscape(spaceID) + "/hierarchy"
		query := asUser(userID)
		query.Set("limit", "100")
		if from != "" {
			query.Set("from", from)
		}
		if err := c.do(ctx, "GET", path, c.asToken, query, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Rooms...)
		if out.NextBatch == "" {
			return all, nil
		}
		from = out.NextBatch
	}
}

func toMessages(events []Event) []Message {
	events = slices.Clone(events)
	slices.Reverse(events)
	msgs := make([]Message, 0, len(events))
	for _, e := range events {
		if !e.IsMessage() || e.Content.Body == "" {
			continue
		}
		msgs = append(msgs, Message{Sender: e.Sender, Body: e.Content.Body})
	}
	return msgs
}
