package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/models"
	"github.com/ellezio/Chat-app-with-Go/internal/services"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/web/components"
	"github.com/gorilla/websocket"
)

type Client struct {
	username string
	conn     *websocket.Conn
}

type ChatHandler struct {
	chatService services.ChatService
	upgrader    websocket.Upgrader
	clients     map[*Client]bool
	broadcast   chan models.Message
}

func newChatHandler(cs services.ChatService) *ChatHandler {
	h := &ChatHandler{
		chatService: cs,
		upgrader:    websocket.Upgrader{},
		clients:     make(map[*Client]bool),
		broadcast:   make(chan models.Message),
	}

	go func() {
		for {
			msg := <-h.broadcast

			for client := range h.clients {
				ctx := context.WithValue(context.Background(), "username", client.username)

				var html bytes.Buffer
				components.
					MessagesList([]models.Message{msg}, true).
					Render(ctx, &html)

				if err := client.conn.WriteMessage(websocket.TextMessage, html.Bytes()); err != nil {
					log.Println(err)
				}
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

	username := session.GetUsername(r.Context())

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
	}

	client := &Client{username, conn}
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

		msg := models.Message{
			Author:  username,
			Content: payload.Msg,
		}

		h.chatService.SaveMessage(msg)
		h.broadcast <- msg
	}

	delete(h.clients, client)
}

func (h *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if !session.IsLoggedIn(r.Context()) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	username := session.GetUsername(r.Context())

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

	msg := models.Message{
		Author:  username,
		Type:    models.FileMessage,
		Content: fileHeader.Filename,
	}

	h.chatService.SaveMessage(msg)
	h.broadcast <- msg
}
