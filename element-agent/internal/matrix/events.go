package matrix

import "slices"

type Mentions struct {
	UserIDs []string `json:"user_ids,omitempty"`
	Room    bool     `json:"room,omitempty"`
}

type Content struct {
	MsgType  string    `json:"msgtype,omitempty"`
	Body     string    `json:"body,omitempty"`
	Mentions *Mentions `json:"m.mentions,omitempty"`
}

type Event struct {
	Type           string  `json:"type"`
	EventID        string  `json:"event_id"`
	RoomID         string  `json:"room_id"`
	Sender         string  `json:"sender"`
	StateKey       *string `json:"state_key,omitempty"`
	OriginServerTS int64   `json:"origin_server_ts"`
	Content        Content `json:"content"`
}

func (e Event) IsMessage() bool {
	return e.Type == "m.room.message" && e.StateKey == nil
}

func (e Event) MentionsUser(userID string) bool {
	if e.Content.Mentions == nil {
		return false
	}
	return slices.Contains(e.Content.Mentions.UserIDs, userID)
}

type Transaction struct {
	Events []Event `json:"events"`
}
