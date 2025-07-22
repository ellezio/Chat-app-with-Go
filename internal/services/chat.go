package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal/message"
)

func NewChatService(db *sql.DB) *ChatService {
	return &ChatService{
		db: db,
	}
}

type ChatService struct {
	db *sql.DB
}

func (s ChatService) GetMessages() []message.Message {
	var msgs []message.Message
	rows, err := s.db.Query("SELECT * FROM messages")
	if err != nil {
		fmt.Println(err)
	} else {
		for rows.Next() {
			var msg message.Message
			if err := rows.Scan(&msg.ID, &msg.Author, &msg.Content, &msg.Type, &msg.CreatedAt, &msg.ModifiedAt); err != nil {
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

func (s ChatService) SaveMessage(msg *message.Message) {
	_, err := s.db.Exec(
		"INSERT INTO messages VALUES (NULL, ?, ?, ?, ?, ?)",
		msg.Author,
		msg.Content,
		msg.Type,
		msg.CreatedAt.Format(time.DateTime),
		msg.ModifiedAt.Format(time.DateTime),
	)

	if err != nil {
		fmt.Println(err)
		return
	}
}
