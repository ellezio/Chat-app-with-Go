package message

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type MessageType string
type MessageStatus string

const (
	TextMessage  MessageType = "text"
	ImageMessage MessageType = "image"

	Sending MessageStatus = "sending"
	Sent    MessageStatus = "sent"
	Error   MessageStatus = "error"
)

type Message struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	Author     string        `bson:"author"`
	Content    string        `bson:"content"`
	Type       MessageType   `bson:"type"`
	CreatedAt  time.Time     `bson:"created_at"`
	ModifiedAt time.Time     `bson:"modified_at"`
	Status     MessageStatus `bson:"status"`
}

func New(author, content string, typ MessageType) *Message {
	t := time.Now()

	return &Message{
		ID:         bson.NilObjectID,
		Author:     author,
		Content:    content,
		Type:       typ,
		CreatedAt:  t,
		ModifiedAt: t,
		Status:     Sending,
	}
}
