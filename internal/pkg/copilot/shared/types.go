package shared

type UserRequest struct {
	SessionId string `json:"-"`
	Seq       int32  `json:"-"`
	Message   string
	FrontPart string
	BackPart  string
	Filename  string
	Workspace string
	History   []Message `json:"history,omitempty"`
}

type ChunkData struct {
	ID      string
	Content string
	IsLast  bool
}

type Message struct {
	Role     string `json:"role"` // "user" or "assistant"
	Content  string `json:"content"`
	Time     int64  `json:"time"`
	Seq      int32  `json:"seq,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}
