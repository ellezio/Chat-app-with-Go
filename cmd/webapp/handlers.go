package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal/chat"
	"github.com/ellezio/Chat-app-with-Go/internal/database"
	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"github.com/ellezio/Chat-app-with-Go/internal/services"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/web/components"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type ChatHandler struct {
	chatService services.ChatService
	upgrader    websocket.Upgrader
	chats       map[string]*chat.Chat
}

var store = &database.MongodbStore{}

func newChatHandler(cs services.ChatService) *ChatHandler {
	h := &ChatHandler{
		chatService: cs,
		upgrader:    websocket.Upgrader{},
		chats:       make(map[string]*chat.Chat),
	}

	chts, err := store.GetChats()
	if err != nil {
		panic(err)
	}

	if len(chts) == 0 {
		cht := chat.New("test1")
		store.SaveChat(cht)
		chts = append(chts, *cht)

		cht = chat.New("test2")
		store.SaveChat(cht)
		chts = append(chts, *cht)
	}

	for _, cht := range chts {
		cht.Store = store
		cht.Start()
		h.chats[cht.ID.Hex()] = &cht
	}

	return h
}

func (self *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	chtId := r.PathValue("chat-id")
	chts := slices.Collect(maps.Values(self.chats))

	if cht, ok := self.chats[chtId]; ok {
		components.Page(chts, cht.GetMessages()).Render(r.Context(), w)
		return
	}

	components.Page(chts, nil).Render(r.Context(), w)
}

func (self *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Fatal(err)
	}

	username := r.PostForm.Get("username")

	if username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), w)
		return
	}

	authData := session.AuthData{Username: username}
	sesh := session.New(authData)
	session.SetSessionCookie(w, sesh)

	w.Write([]byte("<div id='modal' hx-swap-oob='delete'></div>"))
}

func (self *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	if !session.IsLoggedIn(r.Context()) {
		return
	}

	sesh := session.GetSession(r.Context())
	username := sesh.Username

	conn, err := self.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
	}

	client := chat.NewClient(sesh.ID, conn)
	client.OnSendMessage = handleSend
	client.OnUpdateMessage = handleUpdate

	log.Printf("%s Connected\r\n", username)

	var cht *chat.Chat

	for {
		var payload struct {
			Type   string `json:"msg-type"`
			Msg    string `json:"msg"`
			ChatId string `json:"chat-id"`
		}

		_, p, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			break
		}

		err = json.Unmarshal(p, &payload)
		if err != nil {
			log.Println(err)
			continue
		}

		chtId := payload.ChatId

		switch payload.Type {
		case "change-chat":
			if newCht, ok := self.chats[chtId]; ok {
				if newCht != cht {
					if cht != nil {
						cht.DisconnectClient(client)
					}

					cht = newCht
					cht.ConnectClient(client)

					var html bytes.Buffer
					msgs := cht.GetMessages()
					ctx := session.Context(context.Background(), client.SessionID)

					components.ChatWindow(cht, msgs).Render(ctx, &html)

					client.Send(html.Bytes())
				}
			}

		case "send-message":
			if payload.Msg != "" {
				msg := message.New(
					cht.ID,
					username,
					payload.Msg,
					message.TextMessage,
				)
				msg.ChatID = cht.ID

				self.chatService.SaveMessage(msg)

				clientMsg := &chat.ClientMessage{
					Type:            chat.NewMessage,
					Msg:             *msg,
					SenderSessionId: client.SessionID,
				}

				cht.SendMessage(clientMsg)
			}
		}
	}

	if cht != nil {
		cht.DisconnectClient(client)
	}
}

func (self *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if !session.IsLoggedIn(r.Context()) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err := r.ParseMultipartForm(1024)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		// TODO htmx response
		return
	}

	chtId := r.FormValue("chat-id")
	cht, ok := self.chats[chtId]
	if !ok {
		return
	}

	sesh := session.GetSession(r.Context())
	username := sesh.Username

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		// TODO htmx response
		return
	}
	defer file.Close()

	dstFile, err := os.Create("web/files/" + fileHeader.Filename)
	if err != nil {
		log.Fatal(err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, file)
	if err != nil {
		log.Fatal(err)
	}

	msg := message.New(
		cht.ID,
		username,
		fileHeader.Filename,
		message.ImageMessage,
	)

	self.chatService.SaveMessage(msg)

	clientMsg := &chat.ClientMessage{
		Type:            chat.NewMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	cht.SendMessage(clientMsg)
}

func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if !query.Has("msg-id") {
		w.WriteHeader(400)
		return
	}

	msgId := query.Get("msg-id")

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msg, err := store.GetMessage(id)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	components.MessageBox(*msg, true, false).Render(r.Context(), w)
}

func (h *ChatHandler) GetMessageEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	msgId := r.FormValue("msg-id")

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msg, err := store.GetMessage(id)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	components.
		MessageBox(*msg, true, true).
		Render(r.Context(), w)
}

func (self *ChatHandler) PostMessageEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	msgId := r.FormValue("msg-id")
	msgContent := r.FormValue("msg-content")

	chtId := r.FormValue("chat-id")
	cht, ok := self.chats[chtId]
	if !ok {
		return
	}

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msg, err := store.UpdateMessageContent(id, msgContent)

	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	sesh := session.GetSession(r.Context())
	clientMsg := &chat.ClientMessage{
		Type:            chat.UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	cht.SendMessage(clientMsg)
}

func (self *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(501)
}

func (self *ChatHandler) MessageHide(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	chtId := r.FormValue("chat-id")
	cht, ok := self.chats[chtId]
	if !ok {
		return
	}

	doHide, err := strconv.ParseBool(r.PathValue("doHide"))
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msgId := r.FormValue("msg-id")
	sesh := session.GetSession(r.Context())
	user := sesh.Username

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msg, err := store.SetHideMessage(id, user, doHide)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
	}

	clientMsg := &chat.ClientMessage{
		Type:            chat.UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
		OnlySender:      true,
	}

	cht.SendMessage(clientMsg)
}

func (self *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	chtId := r.FormValue("chat-id")
	cht, ok := self.chats[chtId]
	if !ok {
		return
	}

	msgId := r.FormValue("msg-id")
	sesh := session.GetSession(r.Context())

	id, err := bson.ObjectIDFromHex(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(400)
		return
	}

	msg, err := store.DeleteMessage(id)
	if err != nil {
		log.Println(err)
		w.WriteHeader(404)
		return
	}

	clientMsg := &chat.ClientMessage{
		Type:            chat.UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	cht.SendMessage(clientMsg)
}

func handleSend(ctx context.Context, clientMsg *chat.ClientMessage) *bytes.Buffer {
	msg := clientMsg.Msg
	var html bytes.Buffer

	components.
		MessagesList([]message.Message{msg}, true).
		Render(ctx, &html)

	children := components.ContextMenu(msg, false)
	ctx = templ.WithChildren(ctx, children)
	components.ContextMenusWrapper(true).Render(ctx, &html)

	return &html
}

func handleUpdate(ctx context.Context, clientMsg *chat.ClientMessage) *bytes.Buffer {
	msg := clientMsg.Msg
	var html bytes.Buffer

	components.
		MessageBox(clientMsg.Msg, true, false).
		Render(ctx, &html)

	components.
		ContextMenu(msg, true).
		Render(ctx, &html)

	return &html
}
