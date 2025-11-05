package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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
	Id         bson.ObjectID `json:"id"`
	ChatId     bson.ObjectID `json:"chatId"`
	AuthorId   string        `json:"authorId"`
	Content    string        `json:"content"`
	Type       MessageType   `json:"type"`
	CreatedAt  time.Time     `json:"createdAt"`
	ModifiedAt time.Time     `json:"modifiedAt"`
	Status     MessageStatus `json:"status"`
	HiddenFor  []string      `json:"hiddenFor"`
	Deleted    bool          `json:"deleted"`
	Author     User          `json:"author"`
}

func (m *Message) MarshalBinary() ([]byte, error) {
	return json.Marshal(m)
}

func (m *Message) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, m)
}

func New(chatId, authorId, content string, typ MessageType) *Message {
	t := time.Now()

	cId, err := bson.ObjectIDFromHex(chatId)
	if err != nil {
		panic(err)
	}

	return &Message{
		Id:         bson.NilObjectID,
		ChatId:     cId,
		AuthorId:   authorId,
		Content:    content,
		Type:       typ,
		CreatedAt:  t,
		ModifiedAt: t,
		Status:     Sending,
		HiddenFor:  []string{},
		Deleted:    false,
	}
}

type User struct {
	Id   bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name string        `bson:"name"          json:"name"`
}

func (m *User) MarshalBinary() ([]byte, error) {
	return json.Marshal(m)
}

func (m *User) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, m)
}

type EventType int

const (
	Event_NewMessage EventType = iota
	Event_UpdateMessage
	Event_EditMessage
	Event_HideMessage
	Event_DeleteMessage
	Event_PinMessage
	Event_NewChat
)

type MessageEventDetails struct {
	Id      string        `json:"id"`
	Content string        `json:"content"`
	Type    MessageType   `json:"type"`
	Status  MessageStatus `json:"status"`
	Hidden  bool          `json:"hidden"`
	Deleted bool          `json:"deleted"`
}

type ChatEventDetails struct{}

type ChatEvent struct {
	Type    EventType `json:"type"`
	ChatId  string    `json:"chatId"`
	UserId  string    `json:"userId"`
	Details any       `json:"details"`
}

func (ce *ChatEvent) UnmarshalJSON(data []byte) error {
	var temp struct {
		Type    EventType       `json:"type"`
		ChatId  string          `json:"chatId"`
		UserId  string          `json:"userId"`
		Details json.RawMessage `json:"details"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	ce.Type = temp.Type
	ce.ChatId = temp.ChatId
	ce.UserId = temp.UserId

	switch temp.Type {
	case Event_NewMessage, Event_EditMessage, Event_HideMessage, Event_DeleteMessage, Event_PinMessage:
		var details MessageEventDetails
		if err := json.Unmarshal(temp.Details, &details); err != nil {
			return err
		}
		ce.Details = details
	case Event_UpdateMessage:
		var details Message
		if err := json.Unmarshal(temp.Details, &details); err != nil {
			return err
		}
		ce.Details = details
	case Event_NewChat:
		var details Chat
		if err := json.Unmarshal(temp.Details, &details); err != nil {
			return err
		}
		ce.Details = &details
	default:
		return fmt.Errorf("Event \"%v\" not recognised", temp.Type)
	}

	return nil
}

type EventData struct {
	Msg        *Message
	Cht        *Chat
	Connected  bool
	OnlySender bool
	SenderId   string
}

type Client interface {
	GetId() string
	HandleEvent(evt EventType, data EventData)
}

type Store interface {
	GetChats() ([]*Chat, error)
	SaveChat(cht *Chat) error

	GetMessage(msgId string) (*Message, error)
	GetMessages(chatId string) ([]*Message, error)
	SaveMessage(msg *Message) error

	UpdateMessageContent(id string, content string) (*Message, error)
	SetHideMessage(id string, user string, value bool) (*Message, error)
	DeleteMessage(id string) (*Message, error)
}

type Chat struct {
	Id   string
	Name string

	store Store

	connectedClients    map[string]Client
	disconnectedClients map[string]Client
	clientsMutex        sync.Mutex

	publishEvent func(event ChatEvent) error
}

func NewChat(name string, store Store) *Chat {
	return &Chat{
		Id:                  bson.NilObjectID.Hex(),
		Name:                name,
		store:               store,
		connectedClients:    make(map[string]Client),
		disconnectedClients: make(map[string]Client),
	}
}

func (self *Chat) ConnectClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.disconnectedClients, client.GetId())
	self.connectedClients[client.GetId()] = client
}

func (self *Chat) DisconnectClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.connectedClients, client.GetId())
	self.disconnectedClients[client.GetId()] = client
}

func (self *Chat) RemoveClient(client Client) {
	self.clientsMutex.Lock()
	defer self.clientsMutex.Unlock()

	delete(self.connectedClients, client.GetId())
	delete(self.disconnectedClients, client.GetId())
}

func (self *Chat) GetMessages() ([]*Message, error) {
	if self.store == nil {
		return nil, errors.New("Store not set.")
	}

	msgs, err := self.store.GetMessages(self.Id)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

func (self *Chat) NewMessage(message *Message, authorId string) error {
	details := MessageEventDetails{
		Id:      message.Id.Hex(),
		Content: message.Content,
		Type:    message.Type,
		Status:  message.Status,
		Hidden:  false,
		Deleted: message.Deleted,
	}

	event := ChatEvent{
		Type:    Event_NewMessage,
		ChatId:  self.Id,
		UserId:  authorId,
		Details: details,
	}

	return self.publishEvent(event)
}

func (self *Chat) UpdateMessage(message *Message, userId string) error {
	event := ChatEvent{
		Type:    Event_UpdateMessage,
		ChatId:  self.Id,
		UserId:  userId,
		Details: message,
	}

	return self.publishEvent(event)
}

func (self *Chat) UpdateMessageContent(id string, content string) error {
	details := MessageEventDetails{
		Id:      id,
		Content: content,
	}

	event := ChatEvent{
		Type:    Event_EditMessage,
		ChatId:  self.Id,
		Details: details,
	}

	return self.publishEvent(event)
}

func (self *Chat) SetHideMessage(id string, userId string, hide bool) error {
	details := MessageEventDetails{
		Id:     id,
		Hidden: hide,
	}

	event := ChatEvent{
		Type:    Event_HideMessage,
		ChatId:  self.Id,
		UserId:  userId,
		Details: details,
	}

	return self.publishEvent(event)
}

func (self *Chat) DeleteMessage(id string) error {
	details := MessageEventDetails{
		Id:      id,
		Deleted: true,
	}

	event := ChatEvent{
		Type:    Event_DeleteMessage,
		ChatId:  self.Id,
		Details: details,
	}

	return self.publishEvent(event)
}

func (self *Chat) Broadcast(evtType EventType, evtData EventData) {
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

	amqpConn *amqp.Connection
	amqpChan *amqp.Channel
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

func (self *Hub) Start() error {
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		return fmt.Errorf("Failed to connect to RabbitMQ")
	}

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("Failed to open a channel")
	}

	_, err = ch.QueueDeclare(
		"chat_messages", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		return fmt.Errorf("Failed to declare a queue")
	}

	err = ch.ExchangeDeclare(
		"chat_notifications", // name
		"fanout",             // type
		true,                 // durable
		false,                // auto-deleted
		false,                // internal
		false,                // no-wait
		nil,                  // arguments
	)
	if err != nil {
		return fmt.Errorf("Failed to declare an exchange")
	}

	q, err := ch.QueueDeclare(
		"",    // name
		false, // durable
		false, // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("Failed to declare a queue")
	}

	err = ch.QueueBind(
		q.Name,               // queue name
		"",                   // routing key
		"chat_notifications", // exchange
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("Failed to bind a queue")
	}

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		return fmt.Errorf("Failed to register a consumer")
	}

	go func() {
		for d := range msgs {
			log.Printf(" [x] %s", d.Body)
			var event ChatEvent

			var temp struct {
				Type    EventType       `json:"type"`
				ChatId  string          `json:"chatId"`
				UserId  string          `json:"userId"`
				Details json.RawMessage `json:"details"`
			}

			if err := json.Unmarshal(d.Body, &temp); err != nil {
				log.Printf("Failed to parsed delivery message: %v", err)
				continue
			}

			event.Type = temp.Type
			event.ChatId = temp.ChatId
			event.UserId = temp.UserId

			switch event.Type {
			case Event_NewChat:
				var cht *Chat
				if err = json.Unmarshal(temp.Details, &cht); err != nil {
					log.Printf("Cannot process entity with type \"%T\" while adding chat", event.Details)
					continue
				}

				cht.publishEvent = self.PublishEvent
				cht.store = self.store
				cht.connectedClients = make(map[string]Client)
				cht.disconnectedClients = make(map[string]Client)

				self.chatsMutex.Lock()
				self.chats[cht.Id] = cht
				self.chatsMutex.Unlock()

				evtData := EventData{
					Cht: cht,
				}

				self.clientMetasMutex.Lock()
				for _, cliMeta := range self.clientMetas {
					cliMeta.Client.HandleEvent(Event_NewChat, evtData)
				}
				self.clientMetasMutex.Unlock()
			default:
				cht := self.GetChat(event.ChatId)
				if cht == nil {
					log.Printf("Chat id: %q, no clients in this hub, ignoring message", event.ChatId)
					return
				}

				var msg Message
				if err = json.Unmarshal(temp.Details, &msg); err != nil {
					log.Printf("Cannot process entity with type \"%T\" while broadcasting message", event.Details)
					continue
				}

				evt := EventData{
					Msg:      &msg,
					SenderId: event.UserId,
					Cht:      cht,
				}

				cht.Broadcast(event.Type, evt)
			}
		}
	}()

	self.amqpConn = conn
	self.amqpChan = ch

	return err
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
		cht.publishEvent = self.PublishEvent
		self.chats[cht.Id] = cht
	}
	self.chatsMutex.Unlock()

	return nil
}

func (self *Hub) PublishEvent(event ChatEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("Failed to send publish message: %v", err)
	}

	err = self.amqpChan.Publish(
		"",              // exchange
		"chat_messages", // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        body,
		})

	if err != nil {
		return fmt.Errorf("Failed to send publish message: %v", err)
	}

	return nil
}

func (self *Hub) GetChats() []*Chat {
	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	return slices.Collect(maps.Values(self.chats))
}

func (self *Hub) GetChat(chatId string) *Chat {
	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cht, ok := self.chats[chatId]; ok {
		return cht
	}

	return nil
}

func (self *Hub) ConnectClient(chatId string, client Client) (cht *Chat, prevCht *Chat, err error) {
	if client == nil {
		return nil, nil, errors.New("Client cannot be nil.")
	}

	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	cliId := client.GetId()
	cliMeta, ok := self.clientMetas[cliId]
	if !ok {
		cliMeta = &ClientMeta{
			Client:      client,
			CurrentChat: "",
		}

		self.clientMetas[cliId] = cliMeta
	}

	self.DisconnectClientFormChat(cliMeta.CurrentChat, client)
	prevCht = self.chats[cliMeta.CurrentChat]

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	// NOTE: temporal assessment that empty string is initial connection
	if chatId == "" {
		for _, cht = range self.chats {
			cht.DisconnectClient(client)
		}
	}

	if cht, ok := self.chats[chatId]; ok {
		cht.ConnectClient(client)
		cliMeta.CurrentChat = cht.Id
		return cht, prevCht, nil
	}

	return nil, nil, nil
}

func (self *Hub) DisconnectClient(client Client) {
	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	cliId := client.GetId()

	if cliMeta, ok := self.clientMetas[cliId]; ok {
		cliChatID := cliMeta.CurrentChat
		self.DisconnectClientFormChat(cliChatID, client)
	}
}

func (self *Hub) DisconnectClientFormChat(chatId string, client Client) {
	if client == nil {
		return
	}

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cht, ok := self.chats[chatId]; ok {
		cht.DisconnectClient(client)
	}
}

func (self *Hub) RemoveClient(client Client) {
	self.clientMetasMutex.Lock()
	defer self.clientMetasMutex.Unlock()

	self.chatsMutex.Lock()
	defer self.chatsMutex.Unlock()

	if cliMeta, ok := self.clientMetas[client.GetId()]; ok {
		if cht, ok := self.chats[cliMeta.CurrentChat]; ok {
			cht.RemoveClient(client)
		}
	}

	delete(self.clientMetas, client.GetId())
}

func (self *Hub) AddChat(name string) {
	cht := NewChat(name, self.store)

	event := ChatEvent{
		Type:    Event_NewChat,
		Details: cht,
	}

	self.PublishEvent(event)
}
