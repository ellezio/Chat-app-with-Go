package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"github.com/pawellendzion/Chat-app-with-Go/internal/models"
	"github.com/pawellendzion/Chat-app-with-Go/internal/services"
	"github.com/pawellendzion/Chat-app-with-Go/web/components"
)

type Client struct {
	username string
	conn     *websocket.Conn
}

type ChatHandler struct {
	logger      *log.Logger
	chatService services.ChatService
	upgrader    websocket.Upgrader
	clients     map[*Client]bool
	broadcast   chan models.Message
}

func newChatHandler(logger *log.Logger, cs services.ChatService) *ChatHandler {
	h := &ChatHandler{
		logger:      logger,
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
					h.logger.Println(err)
				}
			}
		}
	}()

	return h
}

func (h *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	username := ""
	if cookie, err := r.Cookie("username"); err == nil {
		username = cookie.Value
	}

	ctx := context.WithValue(r.Context(), "username", username)
	components.Page(h.chatService.GetMessages()).Render(ctx, w)
}

func (h *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.logger.Fatal(err)
	}

	if username := r.PostForm.Get("username"); username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), w)
	} else {
		cookie := http.Cookie{
			Name:   "username",
			Value:  username,
			Path:   "/",
			MaxAge: 3600,
		}

		http.SetCookie(w, &cookie)

		w.Write([]byte("<div id='modal' hx-swap-oob='delete'></div>"))
	}
}

func (h *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	var username string
	cookie, err := r.Cookie("username")
	if err != nil {
		h.logger.Println(err)
		return
	}
	username = cookie.Value

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Println(err)
	}

	client := &Client{username, conn}
	h.clients[client] = true

	h.logger.Printf("%s Connected\r\n", username)

	for {
		var payload struct {
			Msg string `json:"msg"`
		}

		_, p, err := conn.ReadMessage()
		if err != nil {
			h.logger.Println(err)
			break
		}

		err = json.Unmarshal(p, &payload)
		if err != nil {
			h.logger.Println(err)
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
	cookie, err := r.Cookie("username")
	if err != nil {
		h.logger.Println(err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	username := cookie.Value

	err = r.ParseMultipartForm(1024)
	if err != nil {
		h.logger.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		// TODO htmx response
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		h.logger.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		// TODO htmx response
		return
	}
	defer file.Close()

	dstFile, err := os.Create("web/files/" + fileHeader.Filename)
	if err != nil {
		h.logger.Fatal(err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, file)
	if err != nil {
		h.logger.Fatal(err)
	}

	msg := models.Message{
		Author:  username,
		Type:    models.FileMessage,
		Content: fileHeader.Filename,
	}

	h.chatService.SaveMessage(msg)
	h.broadcast <- msg
}
