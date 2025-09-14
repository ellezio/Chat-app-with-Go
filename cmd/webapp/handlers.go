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

// TODO: add parsing data in order to validate incomming data correctness and return appropiate messages.
// TODO: manage redirection mostly with HTMX (only, if possible)
// TODO: default layout with HTMX always included to manage browser state
// TODO: add some meaningful repsonse messages

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

func (self *ChatHandler) Homepage(w http.ResponseWriter, r *http.Request) {
	chts := self.hub.GetChats()
	components.Homepage(chts, nil).Render(r.Context(), w)
}

func (self *ChatHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if session.IsLoggedIn(r.Context()) {
		http.Redirect(w, r, "/", 302)
	}

	components.LoginPage().Render(r.Context(), w)
}

func (self *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	username := r.PostForm.Get("username")
	if username == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), w)
		return
	}

	authData := session.AuthData{Username: username}
	sesh := session.New(authData)
	session.SetSessionCookie(w, sesh)

	w.Header().Add("Hx-Redirect", "/")
}

// NOTE:
// maybe it would be good to add redirect through websocket
// and handle session validation. But that is thing for future.
func (self *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	conn, err := self.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	seshID := session.GetSessionID(r.Context())
	client := NewHttpClient(seshID, conn)
	if err = session.SetClientID(seshID, client.GetID()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

	username := session.GetUsername(r.Context())

	log.Printf("%s Connected\r\n", username)

	if _, _, err = self.hub.ConnectClient("", client); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
		return
	}

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
			cht, prevCht, err := self.hub.ConnectClient(chatID, client)
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
			components.ChatListItem(cht, true).Render(ctx, &html)
			if prevCht != nil {
				components.ChatListItem(prevCht, false).Render(ctx, &html)
			}

			client.Send(html.Bytes())

		case "send-message":
			if payload.Msg != "" {
				// TODO: Binary messages
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
	err := r.ParseMultipartForm(1024)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sesh := session.GetSession(r.Context())
	username := sesh.Username

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer file.Close()

	dstFile, err := os.Create("web/files/" + fileHeader.Filename)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, file)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msgId := query.Get("msg-id")

	msg, err := sto.GetMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusInternalServerError)
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
		w.WriteHeader(http.StatusNotFound)
		return
	}

	msg, err := sto.UpdateMessageContent(msgId, msgContent)

	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
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
	w.WriteHeader(http.StatusNotImplemented)
}

func (self *ChatHandler) MessageHide(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	chatID := r.FormValue("chat-id")
	cht := self.hub.GetChat(chatID)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	doHide, err := strconv.ParseBool(r.PathValue("doHide"))
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msgId := r.FormValue("msg-id")
	sesh := session.GetSession(r.Context())
	user := sesh.Username

	msg, err := sto.SetHideMessage(msgId, user, doHide)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
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
		w.WriteHeader(http.StatusNotFound)
		return
	}

	msgId := r.FormValue("msg-id")
	sesh := session.GetSession(r.Context())

	msg, err := sto.DeleteMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
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
