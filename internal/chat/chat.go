package chat

import (
	"bytes"
	"context"
	"log"
	"sync"

	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type ClientMessageType int

const (
	NewMessage ClientMessageType = iota
	UpdateMessage
	NewChat
)

type ClientMessage struct {
	Type            ClientMessageType
	Msg             message.Message
	OnlySender      bool
	SenderSessionId session.SessionID
}

func NewClient(sessionId session.SessionID, conn *websocket.Conn) *Client {
	return &Client{
		id: uuid.NewString(),

		SessionID: sessionId,
		conn:      conn,
		connMux:   sync.Mutex{},

		OnSendMessage:   nil,
		OnUpdateMessage: nil,
	}
}

type Client struct {
	id string

	SessionID session.SessionID
	conn      *websocket.Conn
	connMux   sync.Mutex

	OnSendMessage   func(context.Context, *ClientMessage) *bytes.Buffer
	OnUpdateMessage func(context.Context, *ClientMessage) *bytes.Buffer
	OnNewChat       func(context.Context, *Chat) *bytes.Buffer
}

func (self *Client) SendMessage(event *ClientMessage) {
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

func (self *Client) SendChat(cht *Chat) {
	ctx := session.Context(context.Background(), self.SessionID)
	log.Printf("send to %s\n", session.GetUsername(ctx))
	html := self.OnNewChat(ctx, cht)
	self.Send(html.Bytes())
}

func (self *Client) Send(data []byte) {
	self.connMux.Lock()
	defer self.connMux.Unlock()

	if err := self.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Println(err)
	}
}

func New(name string) *Chat {
	return &Chat{
		bson.NilObjectID,
		name,
		nil,
		make(chan ClientMessage),
		false,

		make(map[string]*Client),
		make(map[string]*Client),
		sync.RWMutex{},
	}
}

type Store interface {
	GetChats() ([]*Chat, error)
	SaveChat(cht *Chat) error

	GetMessage(msgID bson.ObjectID) (*message.Message, error)
	GetMessages(chatID bson.ObjectID) ([]*message.Message, error)
	SaveMessage(chatID bson.ObjectID, msg *message.Message) error
}

type Chat struct {
	ID      bson.ObjectID `bson:"_id,omitempty"`
	Name    string        `bson:"name"`
	Store   Store         `bson:"-"`
	ch      chan ClientMessage
	started bool

	ConnectedClients    map[string]*Client
	DisconnectedClients map[string]*Client
	clientsRWMutex      sync.RWMutex
}

func (self *Chat) Broadcast(clientMsg *ClientMessage) {
	self.clientsRWMutex.RLock()
	defer self.clientsRWMutex.RUnlock()

	for _, client := range self.ConnectedClients {
		client.SendMessage(clientMsg)
	}

	for _, client := range self.DisconnectedClients {
		client.SendMessage(clientMsg)
	}
}

func (self *Chat) Start() {
	self.ch = make(chan ClientMessage)
	self.started = true

	self.ConnectedClients = make(map[string]*Client)
	self.DisconnectedClients = make(map[string]*Client)
}

func (self *Chat) ConnectClient(client *Client) {
	self.clientsRWMutex.Lock()
	defer self.clientsRWMutex.Unlock()

	self.ConnectedClients[client.id] = client
}

func (self *Chat) DisconnectClient(id string) {
	delete(self.ConnectedClients, id)
}

func (self *Chat) SendMessage(clientMsg *ClientMessage) {
	msg := &clientMsg.Msg

	err := self.Store.SaveMessage(self.ID, msg)
	if err != nil {
		log.Println(err)
		msg.Status = message.Error
		clientMsg.OnlySender = true
	}

	self.Broadcast(clientMsg)

	if msg.Status == message.Sending {
		msg.Status = message.Sent
		err := self.Store.SaveMessage(self.ID, msg)
		if err != nil {
			log.Println(err)
			msg.Status = message.Error
		}

		updateMsg := ClientMessage{
			Type:            UpdateMessage,
			Msg:             *msg,
			OnlySender:      true,
			SenderSessionId: clientMsg.SenderSessionId,
		}

		self.Broadcast(&updateMsg)
	}
}

func (self *Chat) GetMessages() []*message.Message {
	msgs, err := self.Store.GetMessages(self.ID)
	if err != nil {
		log.Println(err)
		return nil
	}

	return msgs
}
