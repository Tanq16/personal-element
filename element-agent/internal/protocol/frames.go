package protocol

const (
	TypeHello           = "hello"
	TypeJob             = "job"
	TypeResult          = "result"
	TypeContextRequest  = "context_request"
	TypeContextResponse = "context_response"
	TypeError           = "error"
)

type Claim struct {
	Name  string `json:"name"`
	Claim string `json:"claim"`
}

type Frame struct {
	Type string `json:"type"`

	Claims []Claim `json:"claims,omitempty"`

	JobID   string `json:"job_id,omitempty"`
	Agent   string `json:"agent,omitempty"`
	RoomID  string `json:"room_id,omitempty"`
	EventID string `json:"event_id,omitempty"`
	Body    string `json:"body,omitempty"`
	Limit   int    `json:"limit,omitempty"`

	From string `json:"from,omitempty"`

	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`

	Transcript string `json:"transcript,omitempty"`
	Next       string `json:"next,omitempty"`

	Message string `json:"message,omitempty"`
}
