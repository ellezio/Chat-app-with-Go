package internal

import (
	"errors"
	"log"
	"maps"
	"slices"
	"sync"
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

// At this moment I don't care about it having direct database representaion
type Message struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	ChatID     bson.ObjectID `bson:"chat_id"`
	Author     string        `bson:"author"`
	Content    string        `bson:"content"`
	Type       MessageType   `bson:"type"`
	CreatedAt  time.Time     `bson:"created_at"`
	ModifiedAt time.Time     `bson:"modified_at"`
	Status     MessageStatus `bson:"status"`
	HiddenFor  []string      `bson:"hidden_for"`
	Deleted    bool          `bson:"deleted"`
}

func New(chatID string, author, content string, typ MessageType) *Message {
	t := time.Now()

	cID, err := bson.ObjectIDFromHex(chatID)
	if err != nil {
		panic(err)
	}

	return &Message{
		ID:         bson.NilObjectID,
		ChatID:     cID,
		Author:     author,
		Content:    content,
		Type:       typ,
		CreatedAt:  t,
		ModifiedAt: t,
		Status:     Sending,
		HiddenFor:  []string{},
		Deleted:    false,
	}
}

type EventType int

const (
	Event_NewMessage EventType = iota
	Event_UpdateMessage
	Event_NewChat
)

type EventData struct {
	Msg        *Message
	Cht        *Chat
	Connected  bool
	OnlySender bool
	SenderId   string
}

type Client interface {
	GetID() string
	HandleEvent(evt EventType, data *EventData)
}

type Store interface {
	GetChats() ([]*Chat, error)
	SaveChat(cht *Chat) error

	GetMessage(msgID string) (*Message, error)
	GetMessages(chatID string) ([]*Message, error)
	SaveMessage(msg *Message) error
}

type Chat struct {
	ID   string
	Name string

	store Store

	connectedClients    map[string]Client
	disconnectedClients map[string]Client
	clientsMutex        sync.Mutex
}

func NewChat(name string, store Store) *Chat {
	return &Chat{
		ID:                  "",
		Name:                name,
		store:               store,
		connectedClients:    make(map[string]Client),
		disconnectedClients: make(map[string]Client),
	}
}

func (self *Chat) ConnectClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.disconnectedClients, client.GetID())
	self.connectedClients[client.GetID()] = client
}

func (self *Chat) DisconnectClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.connectedClients, client.GetID())
	self.disconnectedClients[client.GetID()] = client
}

func (self *Chat) RemoveClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.connectedClients, client.GetID())
	delete(self.disconnectedClients, client.GetID())
}

func (self *Chat) GetMessages() ([]*Message, error) {
	if self.store == nil {
		return nil, errors.New("Store not set.")
	}

	msgs, err := self.store.GetMessages(self.ID)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

func (self *Chat) NewMessage(evtData *EventData) {
	msg := evtData.Msg
	evtData.Cht = self

	err := self.store.SaveMessage(msg)
	if err != nil {
		log.Println(err)
		msg.Status = Error
		evtData.OnlySender = true
	}

	self.Broadcast(Event_NewMessage, evtData)

	if msg.Status == Sending {
		msg.Status = Sent
		err := self.store.SaveMessage(msg)
		if err != nil {
			log.Println(err)
			msg.Status = Error
		}

		updateEvtData := &EventData{
			Msg:        msg,
			OnlySender: true,
			SenderId:   evtData.SenderId,
			Cht:        evtData.Cht,
		}

		self.UpdateMessage(updateEvtData)
	}
}

func (self *Chat) UpdateMessage(evtData *EventData) {
	msg := evtData.Msg
	evtData.Cht = self

	err := self.store.SaveMessage(msg)
	if err != nil {
		log.Println(err)
		msg.Status = Error
		evtData.Cht = self
	}

	self.Broadcast(Event_UpdateMessage, evtData)
}

func (self *Chat) Broadcast(evtType EventType, evtData *EventData) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	evtData.Connected = true
	for _, client := range self.connectedClients {
		client.HandleEvent(evtType, evtData)
	}

	evtData.Connected = false
	for _, client := range self.disconnectedClients {
		client.HandleEvent(evtType, evtData)
	}
}

type ClientMeta struct {
	Client      Client
	CurrentChat string
}

type Hub struct {
	store Store

	clientMetas      map[string]*ClientMeta
	clientMetasMutex sync.Mutex

	chats      map[string]*Chat
	chatsMutex sync.Mutex
}

func NewHub(store Store) *Hub {
	return &Hub{
		store: store,

		clientMetas:      make(map[string]*ClientMeta),
		clientMetasMutex: sync.Mutex{},

		chats:      make(map[string]*Chat),
		chatsMutex: sync.Mutex{},
	}
}

func (self *Hub) LoadChatsFromStore() error {
	if self.store == nil {
		return errors.New("Store not set.")
	}

	chts, err := self.store.GetChats()
	if err != nil {
		return errors.Join(errors.New("Failed to load chats."), err)
	}

	self.chatsMutex.Lock()
	for _, cht := range chts {
		self.chats[cht.ID] = cht
	}
	self.chatsMutex.Unlock()

	return nil
}

func (self *Hub) GetChats() []*Chat {
	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	return slices.Collect(maps.Values(self.chats))
}

func (self *Hub) GetChat(chatID string) *Chat {
	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cht, ok := self.chats[chatID]; ok {
		return cht
	}

	return nil
}

func (self *Hub) ConnectClient(chatID string, client Client) (cht *Chat, prevCht *Chat, err error) {
	if client == nil {
		return nil, nil, errors.New("Client cannot be nil.")
	}

	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	cliID := client.GetID()
	cliMeta, ok := self.clientMetas[cliID]
	if !ok {
		cliMeta = &ClientMeta{
			Client:      client,
			CurrentChat: "",
		}

		self.clientMetas[cliID] = cliMeta
	}

	self.DisconnectClientFormChat(cliMeta.CurrentChat, client)
	prevCht = self.chats[cliMeta.CurrentChat]

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	// NOTE: temporal assessment that empty string is initial connection
	if chatID == "" {
		for _, cht = range self.chats {
			cht.DisconnectClient(client)
		}
	}

	if cht, ok := self.chats[chatID]; ok {
		cht.ConnectClient(client)
		cliMeta.CurrentChat = cht.ID
		return cht, prevCht, nil
	}

	return nil, nil, nil
}

func (self *Hub) DisconnectClient(client Client) {
	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	cliID := client.GetID()

	if cliMeta, ok := self.clientMetas[cliID]; ok {
		cliChatID := cliMeta.CurrentChat
		self.DisconnectClientFormChat(cliChatID, client)
	}
}

func (self *Hub) DisconnectClientFormChat(chatID string, client Client) {
	if client == nil {
		return
	}

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cht, ok := self.chats[chatID]; ok {
		cht.DisconnectClient(client)
	}
}

func (self *Hub) RemoveClient(client Client) {
	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cliMeta, ok := self.clientMetas[client.GetID()]; ok {
		if cht, ok := self.chats[cliMeta.CurrentChat]; ok {
			cht.RemoveClient(client)
		}
	}

	delete(self.clientMetas, client.GetID())
}

func (self *Hub) AddChat(name string) {
	cht := NewChat(name, self.store)

	if self.store != nil {
		self.store.SaveChat(cht)
	}

	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	self.chats[cht.ID] = cht

	evtData := &EventData{
		Cht: cht,
	}

	for _, cliMeta := range self.clientMetas {
		cliMeta.Client.HandleEvent(Event_NewChat, evtData)
	}
}
