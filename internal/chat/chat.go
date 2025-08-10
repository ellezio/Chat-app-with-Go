package chat

import (
	"bytes"
	"context"
	"log"
	"sync"

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal/database"
	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/web/components"
	"github.com/gorilla/websocket"
)

type ClientMessageType int

const (
	NewMessage ClientMessageType = iota
	UpdateMessage
)

type ClientMessage struct {
	Type            ClientMessageType
	Msg             message.Message
	OnlySender      bool
	SenderSessionId session.SessionID
}

func NewClient(sessionId session.SessionID, conn *websocket.Conn) *Client {
	return &Client{sessionId, conn, sync.Mutex{}}
}

type Client struct {
	SessionID session.SessionID
	conn      *websocket.Conn
	connMux   sync.Mutex
}

func (self *Client) SendMessage(data []byte) {
	self.connMux.Lock()
	defer self.connMux.Unlock()

	if err := self.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Println(err)
	}
}

func New() *Chat {
	return &Chat{
		make(map[*Client]bool),
		make(chan ClientMessage),
		false,
	}
}

type Chat struct {
	clients map[*Client]bool
	ch      chan ClientMessage
	started bool
}

func (self *Chat) Start() {
	self.ch = make(chan ClientMessage)
	self.started = true

	go func() {
		for {
			clientMsg := <-self.ch
			msg := clientMsg.Msg

			for client := range self.clients {
				ctx := session.Context(context.Background(), client.SessionID)

				var html bytes.Buffer

				switch clientMsg.Type {
				case NewMessage:
					components.
						MessagesList([]message.Message{msg}, true).
						Render(ctx, &html)

					children := components.ContextMenu(msg, false)
					ctx = templ.WithChildren(ctx, children)
					components.ContextMenusWrapper(true).Render(ctx, &html)

				case UpdateMessage:
					if clientMsg.OnlySender && client.SessionID != clientMsg.SenderSessionId {
						continue
					}

					components.
						MessageBox(clientMsg.Msg, true, false).
						Render(ctx, &html)

					components.
						ContextMenu(msg, true).
						Render(ctx, &html)
				}

				client.SendMessage(html.Bytes())
			}

			if msg.Status == message.Sending {
				go func(msg message.Message, senderSessionId session.SessionID) {
					err := database.UpdateStatus(msg.ID, message.Sent)
					if err != nil {
						log.Println(err)
						msg.Status = message.Error
					} else {
						msg.Status = message.Sent
					}

					updateMsg := ClientMessage{
						Type:            UpdateMessage,
						Msg:             msg,
						OnlySender:      true,
						SenderSessionId: senderSessionId,
					}

					self.ch <- updateMsg
				}(msg, clientMsg.SenderSessionId)
			}
		}
	}()
}

func (self *Chat) ConnectClient(client *Client) {
	self.clients[client] = true

	var html bytes.Buffer
	msgs := self.GetMessages()
	ctx := session.Context(context.Background(), client.SessionID)

	components.
		MessagesList(msgs, true).
		Render(ctx, &html)

	children := make([]templ.Component, 0, 10)
	for _, msg := range msgs {
		children = append(children, components.ContextMenu(msg, false))
	}

	ctx = templ.WithChildren(ctx, templ.Join(children...))
	components.ContextMenusWrapper(true).Render(ctx, &html)

	client.SendMessage(html.Bytes())
}

func (self *Chat) DisconnectClient(client *Client) {
	delete(self.clients, client)
}

func (self *Chat) SendMessage(msg *ClientMessage) {
	self.ch <- *msg
}

func (self *Chat) GetMessages() []message.Message {
	msgs, err := database.GetMessages()
	if err != nil {
		log.Println(err)
		return nil
	}

	return msgs
}
