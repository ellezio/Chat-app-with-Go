package models

type MessageType int

const (
	TextMessage MessageType = iota
	FileMessage
)

type Message struct {
	ID      int
	Author  string
	Content string
	Type    MessageType
}
