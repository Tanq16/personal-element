package matrix

import (
	"context"
	"net/url"
	"strconv"
)

type AdminRoom struct {
	RoomID   string `json:"room_id"`
	Name     string `json:"name"`
	RoomType string `json:"room_type"`
}

func (c *Client) Spaces(ctx context.Context) ([]AdminRoom, error) {
	var spaces []AdminRoom
	from := 0
	for {
		var out struct {
			Rooms      []AdminRoom `json:"rooms"`
			NextBatch  int         `json:"next_batch"`
			TotalRooms int         `json:"total_rooms"`
		}
		query := url.Values{
			"from":  {strconv.Itoa(from)},
			"limit": {"100"},
		}
		if err := c.do(ctx, "GET", "/_synapse/admin/v1/rooms", c.adminToken, query, nil, &out); err != nil {
			return nil, err
		}
		for _, room := range out.Rooms {
			if room.RoomType == "m.space" {
				spaces = append(spaces, room)
			}
		}
		if len(out.Rooms) == 0 || from+len(out.Rooms) >= out.TotalRooms {
			return spaces, nil
		}
		from += len(out.Rooms)
	}
}

func (c *Client) AdminJoin(ctx context.Context, roomID, userID string) error {
	body := struct {
		UserID string `json:"user_id"`
	}{UserID: userID}
	path := "/_synapse/admin/v1/join/" + url.PathEscape(roomID)
	return c.do(ctx, "POST", path, c.adminToken, nil, body, nil)
}
