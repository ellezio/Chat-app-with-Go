package services

import (
	"log"

	"github.com/ellezio/Chat-app-with-Go/internal/database"
	"github.com/ellezio/Chat-app-with-Go/internal/message"
)

func NewChatService() *ChatService {
	return &ChatService{}
}

type ChatService struct {
}

func (s ChatService) SaveMessage(msg *message.Message) {
	err := database.SaveMessage(msg)

	if err != nil {
		log.Println(err)
		return
	}
}
