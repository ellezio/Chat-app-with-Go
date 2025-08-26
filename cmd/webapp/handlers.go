package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
	"github.com/ellezio/Chat-app-with-Go/web/components"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ChatHandler struct {
	upgrader websocket.Upgrader
	hub      *internal.Hub
}

var sto = &store.MongodbStore{}

func newChatHandler() *ChatHandler {
	h := &ChatHandler{
		upgrader: websocket.Upgrader{},
		hub:      internal.NewHub(sto),
	}

	h.hub.LoadChatsFromStore()
	return h
}

func (self *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	chts := self.hub.GetChats()
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

	conn, err := self.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
	}

	seshID := session.GetSessionID(r.Context())
	client := NewHttpClient(seshID, conn)
	_ = session.SetClientID(seshID, client.GetID())
	username := session.GetUsername(r.Context())

	log.Printf("%s Connected\r\n", username)

	self.hub.ConnectClient("", client)

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

		chatID := payload.ChatId

		switch payload.Type {
		case "change-chat":
			cht, err := self.hub.ConnectClient(chatID, client)
			if err != nil {
				log.Printf("Failed to connect client. %v\n", err)
				return
			}

			msgs, err := cht.GetMessages()
			if err != nil {
				log.Printf("Change chat: Failed to get messages. %v\n", err)
				return
			}

			ctx := session.Context(context.Background(), client.SessionID)

			var html bytes.Buffer
			components.ChatWindow(cht.ID, msgs).Render(ctx, &html)

			client.Send(html.Bytes())

		case "send-message":
			if payload.Msg != "" {
				msg := internal.New(
					chatID,
					username,
					payload.Msg,
					internal.TextMessage,
				)

				evtData := &internal.EventData{
					Msg:      msg,
					SenderId: client.GetID(),
				}

				cht := self.hub.GetChat(chatID)
				cht.NewMessage(evtData)
			}
		}
	}

	self.hub.DisconnectClient(client)
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

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
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

	msg := internal.New(
		cht.ID,
		username,
		fileHeader.Filename,
		internal.ImageMessage,
	)

	clientMsg := &internal.EventData{
		Msg:      msg,
		SenderId: sesh.ClientID,
	}

	cht.NewMessage(clientMsg)
}

func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if !query.Has("msg-id") {
		w.WriteHeader(400)
		return
	}

	msgId := query.Get("msg-id")

	msg, err := sto.GetMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	components.MessageBox(msg, true, false).Render(r.Context(), w)
}

func (h *ChatHandler) GetMessageEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	msgId := r.FormValue("msg-id")

	msg, err := sto.GetMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	components.
		MessageBox(msg, true, true).
		Render(r.Context(), w)
}

func (self *ChatHandler) PostMessageEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	msgId := r.FormValue("msg-id")
	msgContent := r.FormValue("msg-content")

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
		return
	}

	msg, err := sto.UpdateMessageContent(msgId, msgContent)

	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	sesh := session.GetSession(r.Context())
	clientMsg := &internal.EventData{
		Msg:      msg,
		SenderId: sesh.ClientID,
	}

	cht.UpdateMessage(clientMsg)
}

func (self *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(501)
}

func (self *ChatHandler) MessageHide(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
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

	msg, err := sto.SetHideMessage(msgId, user, doHide)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
	}

	clientMsg := &internal.EventData{
		Msg:        msg,
		SenderId:   sesh.ClientID,
		OnlySender: true,
	}

	cht.UpdateMessage(clientMsg)
}

func (self *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
		return
	}

	msgId := r.FormValue("msg-id")
	sesh := session.GetSession(r.Context())

	msg, err := sto.DeleteMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(404)
		return
	}

	clientMsg := &internal.EventData{
		Msg:      msg,
		SenderId: sesh.ClientID,
	}

	cht.UpdateMessage(clientMsg)
}

func (self *ChatHandler) CreateChat(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	chatName := r.FormValue("chat-name")
	self.hub.AddChat(chatName)
}

func NewHttpClient(sessionId session.SessionID, conn *websocket.Conn) *HttpClient {
	return &HttpClient{
		id:        uuid.NewString(),
		SessionID: sessionId,
		conn:      conn,
		connMux:   sync.Mutex{},
	}
}

type HttpClient struct {
	id        string
	SessionID session.SessionID

	conn    *websocket.Conn
	connMux sync.Mutex
}

func (self *HttpClient) GetID() string { return self.id }

func (self *HttpClient) HandleEvent(evtType internal.EventType, evtData *internal.EventData) {
	ctx := session.Context(context.Background(), self.SessionID)
	var html bytes.Buffer

	switch evtType {
	case internal.Event_NewMessage:
		msg := evtData.Msg

		components.
			MessagesList([]*internal.Message{msg}, true).
			Render(ctx, &html)

		children := components.ContextMenu(msg, false)
		ctx = templ.WithChildren(ctx, children)
		components.ContextMenusWrapper(true).Render(ctx, &html)

	case internal.Event_UpdateMessage:
		if evtData.OnlySender && self.id != evtData.SenderId {
			break
		}

		msg := evtData.Msg

		components.
			MessageBox(msg, true, false).
			Render(ctx, &html)

		components.
			ContextMenu(msg, true).
			Render(ctx, &html)
	case internal.Event_NewChat:
		cht := evtData.Cht

		components.
			ChatList([]*internal.Chat{cht}).
			Render(ctx, &html)
	}

	self.Send(html.Bytes())
}

func (self *HttpClient) Send(data []byte) {
	self.connMux.Lock()
	defer self.connMux.Unlock()

	if err := self.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Println(err)
	}
}
