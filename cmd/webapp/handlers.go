package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

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

func newChatHandler(cs services.ChatService) *ChatHandler {
	h := &ChatHandler{
		chatService: cs,
		upgrader:    websocket.Upgrader{},
		chats:       make(map[string]*chat.Chat),
	}

	h.chats["test"] = chat.New()
	h.chats["test"].Start()

	return h
}

func (self *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	chtId := r.PathValue("chat-id")
	if cht, ok := self.chats[chtId]; ok {
		components.Page(cht.GetMessages()).Render(r.Context(), w)
		return
	}

	components.Page(nil).Render(r.Context(), w)
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

	log.Printf("%s Connected\r\n", username)

	var cht *chat.Chat

	for {
		var payload struct {
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
		if newCht, ok := self.chats[chtId]; ok {
			if newCht != cht {
				if cht != nil {
					cht.DisconnectClient(client)
				}
				cht = newCht
				cht.ConnectClient(client)
			}
		} else {
			continue
		}

		if payload.Msg != "" {
			msg := message.New(
				username,
				payload.Msg,
				message.TextMessage,
			)

			self.chatService.SaveMessage(msg)

			clientMsg := &chat.ClientMessage{
				Type:            chat.NewMessage,
				Msg:             *msg,
				SenderSessionId: client.SessionID,
			}

			cht.SendMessage(clientMsg)
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

	msg, err := database.GetMessage(id)
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

	msg, err := database.GetMessage(id)
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

	msg, err := database.UpdateContent(id, msgContent)
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

	msg, err := database.SetHideMessage(id, user, doHide)
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

	msg, err := database.DeleteMessage(id)
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
