package chat

import (
	"bytes"
	"context"
	"log"
	"sync"

	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"
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
	return &Client{
		SessionID: sessionId,
		conn:      conn,
		connMux:   sync.Mutex{},

		OnSendMessage:   nil,
		OnUpdateMessage: nil,
	}
}

type Client struct {
	SessionID session.SessionID
	conn      *websocket.Conn
	connMux   sync.Mutex

	OnSendMessage   func(context.Context, *ClientMessage) *bytes.Buffer
	OnUpdateMessage func(context.Context, *ClientMessage) *bytes.Buffer
}

func (self *Client) SendMessage(event *ClientMessage) {
	self.connMux.Lock()
	defer self.connMux.Unlock()

	ctx := session.Context(context.Background(), self.SessionID)
	var html *bytes.Buffer

	switch event.Type {
	case NewMessage:
		html = self.OnSendMessage(ctx, event)

	case UpdateMessage:
		if event.OnlySender && self.SessionID != event.SenderSessionId {
			break
		}

		html = self.OnUpdateMessage(ctx, event)
	}

	if html == nil {
		return
	}

	self.Send(html.Bytes())
}

func (self *Client) Send(data []byte) {
	if err := self.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Println(err)
	}
}

func New(name string) *Chat {
	return &Chat{
		bson.NilObjectID,
		name,
		nil,
		make(map[*Client]bool),
		make(chan ClientMessage),
		false,
	}
}

type Store interface {
	GetMessage(msgID bson.ObjectID) (*message.Message, error)
	GetMessages(chatID bson.ObjectID) ([]message.Message, error)
	SaveMessage(chatID bson.ObjectID, msg *message.Message) error
}

type Chat struct {
	ID      bson.ObjectID `bson:"_id,omitempty"`
	Name    string        `bson:"name"`
	Store   Store         `bson:"-"`
	clients map[*Client]bool
	ch      chan ClientMessage
	started bool
}

func (self *Chat) Start() {
	self.clients = make(map[*Client]bool)
	self.ch = make(chan ClientMessage)
	self.started = true

	go func() {
		for {
			clientMsg := <-self.ch

			for client := range self.clients {
				client.SendMessage(&clientMsg)
			}

			msg := clientMsg.Msg
			if msg.Status == message.Sending {
				go func(msg message.Message, senderSessionId session.SessionID) {
					msg.Status = message.Sent
					err := self.Store.SaveMessage(self.ID, &msg)
					if err != nil {
						log.Println(err)
						msg.Status = message.Error
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
}

func (self *Chat) DisconnectClient(client *Client) {
	delete(self.clients, client)
}

func (self *Chat) SendMessage(msg *ClientMessage) {
	self.ch <- *msg
}

func (self *Chat) GetMessages() []message.Message {
	msgs, err := self.Store.GetMessages(self.ID)
	if err != nil {
		log.Println(err)
		return nil
	}

	return msgs
}
