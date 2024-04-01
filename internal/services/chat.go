package services

import (
	"database/sql"
	"fmt"

	"github.com/pawellendzion/Chat-app-with-Go/internal/models"
)

func NewChatService(db *sql.DB) *ChatService {
	return &ChatService{
		db: db,
	}
}

type ChatService struct {
	db *sql.DB
}

func (s ChatService) GetMessages() []models.Message {
	var msgs []models.Message
	rows, err := s.db.Query("SELECT * FROM messages")
	if err != nil {
		fmt.Println(err)
	} else {
		for rows.Next() {
			var msg models.Message
			if err := rows.Scan(&msg.ID, &msg.Author, &msg.Content); err != nil {
				fmt.Println(err)
				break
			}
			msgs = append(msgs, msg)
		}

		if err := rows.Err(); err != nil {
			fmt.Println(err)
		}
	}

	return msgs
}

func (s ChatService) SaveMessage(msg models.Message) {
	_, err := s.db.Exec("INSERT INTO messages VALUES (NULL, ?, ?)", msg.Author, msg.Content)
	if err != nil {
		fmt.Println(err)
		return
	}
}
