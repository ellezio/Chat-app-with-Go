package message

import "time"

type MessageType int

const (
	TextMessage MessageType = iota
	ImageMessage
)

type Message struct {
	ID         int
	Author     string
	Content    string
	Type       MessageType
	CreatedAt  time.Time
	ModifiedAt time.Time
}

func New(author, content string, typ MessageType) *Message {
	t := time.Now()

	return &Message{
		ID:         0,
		Author:     author,
		Content:    content,
		Type:       typ,
		CreatedAt:  t,
		ModifiedAt: t,
	}
}
