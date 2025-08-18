package chat

import (
	"maps"
	"slices"
	"sync"

	"github.com/ellezio/Chat-app-with-Go/internal/message"
)

type ChatServer struct {
	Store Store

	clientsChat        map[string]string
	clientsChatRWMutex sync.RWMutex

	clients        map[string]*Client
	clientsRWMutex sync.RWMutex

	chats        map[string]*Chat
	chatsRWMutex sync.RWMutex
}

func NewChatServer(store Store) *ChatServer {
	srv := &ChatServer{
		Store: store,

		clientsChat: make(map[string]string),

		clients: make(map[string]*Client),

		chats:        make(map[string]*Chat),
		chatsRWMutex: sync.RWMutex{},
	}

	chts, err := store.GetChats()
	if err != nil {
		panic(err)
	}

	if len(chts) == 0 {
		cht := New("test1")
		store.SaveChat(cht)
		chts = append(chts, cht)

		cht = New("test2")
		store.SaveChat(cht)
		chts = append(chts, cht)
	}

	for _, cht := range chts {
		cht.Store = store
		cht.Start()
		srv.chats[cht.ID.Hex()] = cht
	}

	return srv
}

func (self *ChatServer) GetChats() []*Chat {
	self.chatsRWMutex.RLock()
	defer self.chatsRWMutex.RUnlock()

	return slices.Collect(maps.Values(self.chats))
}

func (self *ChatServer) GetChat(chatID string) *Chat {
	self.chatsRWMutex.RLock()
	defer self.chatsRWMutex.RUnlock()

	if cht, ok := self.chats[chatID]; ok {
		return cht
	}

	return nil
}

func (self *ChatServer) GetMessages(chatID string) []*message.Message {
	if cht, ok := self.chats[chatID]; ok {
		return cht.GetMessages()
	}

	return nil
}

func (self *ChatServer) ConnectClient(chatID string, client *Client) *Chat {
	self.chatsRWMutex.RLock()
	defer self.chatsRWMutex.RUnlock()

	self.clientsChatRWMutex.RLock()
	defer self.clientsChatRWMutex.RUnlock()

	self.clientsRWMutex.Lock()
	if _, ok := self.clients[client.id]; !ok {
		self.clients[client.id] = client
	}
	self.clientsRWMutex.Unlock()

	self.DisconnectClientFromChat(client)

	if cht, ok := self.chats[chatID]; ok {
		cht.ConnectClient(client)
		self.clientsChat[client.id] = cht.ID.Hex()
		return cht
	}

	return nil
}

func (self *ChatServer) DisconnectClient(client *Client) {
	self.DisconnectClientFromChat(client)

	self.clientsRWMutex.Lock()
	delete(self.clients, client.id)
	self.clientsRWMutex.Unlock()
}

func (self *ChatServer) DisconnectClientFromChat(client *Client) {
	self.chatsRWMutex.RLock()
	defer self.chatsRWMutex.RUnlock()

	self.clientsChatRWMutex.RLock()
	defer self.clientsChatRWMutex.RUnlock()

	if chatID, ok := self.clientsChat[client.id]; ok {
		if cht, ok := self.chats[chatID]; ok {
			cht.DisconnectClient(client.id)
		}
	}

	self.clientsChat[client.id] = ""
}

func (self *ChatServer) AddChat(name string) {
	cht := New(name)
	cht.Store = self.Store
	self.Store.SaveChat(cht)
	self.chats[cht.ID.Hex()] = cht

	self.broadcastChat(cht)
}

func (self *ChatServer) broadcastChat(cht *Chat) {
	self.clientsRWMutex.RLock()
	defer self.clientsRWMutex.RUnlock()

	for _, client := range self.clients {
		client.SendChat(cht)
	}
}
