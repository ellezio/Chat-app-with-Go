package services

import "github.com/pawellendzion/Chat-app-with-Go/internal/components"

func NewChatService() *ChatService {
	return &ChatService{
		msgs: &[]components.Message{},
	}
}

type ChatService struct {
	msgs *[]components.Message
}

func (s ChatService) GetMessages() []components.Message {
	return *s.msgs
}

func (s ChatService) SaveMessage(msg components.Message) {
	*s.msgs = append(*s.msgs, msg)
}
