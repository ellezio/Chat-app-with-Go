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

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal/database"
	"github.com/ellezio/Chat-app-with-Go/internal/message"
	"github.com/ellezio/Chat-app-with-Go/internal/services"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/web/components"
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

type Client struct {
	sessionID session.SessionID
	conn      *websocket.Conn
}

type ChatHandler struct {
	chatService services.ChatService
	upgrader    websocket.Upgrader
	clients     map[*Client]bool
	broadcast   chan ClientMessage
}

func newChatHandler(cs services.ChatService) *ChatHandler {
	h := &ChatHandler{
		chatService: cs,
		upgrader:    websocket.Upgrader{},
		clients:     make(map[*Client]bool),
		broadcast:   make(chan ClientMessage),
	}

	go func() {
		for {
			clientMsg := <-h.broadcast
			msg := clientMsg.Msg

			for client := range h.clients {
				ctx := session.Context(context.Background(), client.sessionID)

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
					if clientMsg.OnlySender && client.sessionID != clientMsg.SenderSessionId {
						continue
					}

					components.
						MessageBox(clientMsg.Msg, true, false).
						Render(ctx, &html)

					components.
						ContextMenu(msg, true).
						Render(ctx, &html)
				}

				if err := client.conn.WriteMessage(websocket.TextMessage, html.Bytes()); err != nil {
					log.Println(err)
				}
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

					h.broadcast <- updateMsg
				}(msg, clientMsg.SenderSessionId)
			}
		}
	}()

	return h
}

func (h *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	components.Page(h.chatService.GetMessages()).Render(r.Context(), w)
}

func (h *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
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

func (h *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	if !session.IsLoggedIn(r.Context()) {
		return
	}

	sesh := session.GetSession(r.Context())
	username := sesh.Username

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
	}

	client := &Client{sesh.ID, conn}
	h.clients[client] = true

	log.Printf("%s Connected\r\n", username)

	for {
		var payload struct {
			Msg string `json:"msg"`
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

		msg := message.New(
			username,
			payload.Msg,
			message.TextMessage,
		)

		h.chatService.SaveMessage(msg)

		clientMsg := ClientMessage{
			Type:            NewMessage,
			Msg:             *msg,
			SenderSessionId: client.sessionID,
		}

		h.broadcast <- clientMsg
	}

	delete(h.clients, client)
}

func (h *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if !session.IsLoggedIn(r.Context()) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	sesh := session.GetSession(r.Context())
	username := sesh.Username

	err := r.ParseMultipartForm(1024)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		// TODO htmx response
		return
	}

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

	h.chatService.SaveMessage(msg)

	clientMsg := ClientMessage{
		Type:            NewMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	h.broadcast <- clientMsg
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

func (h *ChatHandler) PostMessageEdit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	msgId := r.FormValue("msg-id")
	msgContent := r.FormValue("msg-content")

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
	clientMsg := ClientMessage{
		Type:            UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	h.broadcast <- clientMsg
}

func (h *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) {
}

func (h *ChatHandler) MessageHide(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
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

	clientMsg := ClientMessage{
		Type:            UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
		OnlySender:      true,
	}

	h.broadcast <- clientMsg
}

func (h *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

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

	log.Println(msg)

	clientMsg := ClientMessage{
		Type:            UpdateMessage,
		Msg:             *msg,
		SenderSessionId: sesh.ID,
	}

	h.broadcast <- clientMsg
}
