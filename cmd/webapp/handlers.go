package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
	"github.com/ellezio/Chat-app-with-Go/web/components"
	"github.com/gorilla/websocket"
)

// TODO: add parsing data in order to validate incomming data correctness and return appropiate messages.
// TODO: manage redirection mostly with HTMX (only, if possible)
// TODO: default layout with HTMX always included to manage browser state
// TODO: add some meaningful repsonse messages

type ChatHandler struct {
	upgrader     websocket.Upgrader
	hub          *internal.Hub
	fileUploader FileUploader
	logger       *slog.Logger
	store        internal.Store
}

func newChatHandler(store internal.Store, logger *slog.Logger) (*ChatHandler, *internal.Hub) {
	h := &ChatHandler{
		upgrader: websocket.Upgrader{},
		hub:      internal.NewHub(store),
		logger:   logger,
		store:    store,
	}

	h.hub.LoadChatsFromStore()
	return h, h.hub
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

func (self *ChatHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if session.IsLoggedIn(r.Context()) {
		http.Redirect(w, r, "/", 302)
	}

	components.RegisterPage().Render(r.Context(), w)
}

func (self *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		LoggerFromContext(r.Context(), self.logger).Error("Failed to parse login form", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	wr := bufio.NewWriter(w)
	username := r.PostForm.Get("username")
	if username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), wr)
	}

	password := r.PostForm.Get("password")
	if password == "" {
		components.ErrorMsg("password", "Fill the field").Render(r.Context(), wr)
	}

	if username == "" || password == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		wr.Flush()
		return
	}

	user, err := self.store.GetUser(username)
	if err != nil || !user.CheckPass(password) {
		w.WriteHeader(http.StatusUnauthorized)
		components.ErrorMsg("login", "username or password is invalid").Render(r.Context(), w)
		return
	}

	userData := session.UserData{
		Id:   user.Id.Hex(),
		Name: user.Name,
	}

	sesh := session.New()
	sesh.User = userData
	sesh.Save()
	session.SetSessionCookie(w, sesh)

	w.Header().Add("Hx-Redirect", "/")
}

func (self *ChatHandler) Register(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), self.logger)
	if err := r.ParseForm(); err != nil {
		logger.Error("Failed to parse register form", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	wr := bufio.NewWriter(w)
	username := r.PostForm.Get("username")
	if username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), wr)
	}

	password := r.PostForm.Get("password")
	if password == "" {
		components.ErrorMsg("password", "Fill the field").Render(r.Context(), wr)
	}

	if username == "" || password == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		wr.Flush()
		return
	}

	user, err := self.store.GetUser(username)
	if err != nil && !errors.Is(err, store.ErrNoRecord) {
		logger.Error("Can't find user", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	} else if user != nil {
		w.WriteHeader(http.StatusUnauthorized)
		components.ErrorMsg("register", "user already exists").Render(r.Context(), w)
		return
	}

	user = internal.NewUser(username, password)
	if err := self.store.CreateUser(user); err != nil {
		logger.Error("Can't create user", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Add("Hx-Redirect", "/login")
}
func (self *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), self.logger)

	// TODO: proper check
	self.upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := self.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade connection to websocket", slog.Any("error", err))
		return
	}
	defer conn.Close()

	sesh := session.GetSession(r.Context())
	client := NewHttpClient(conn, sesh.Id, logger)

	logger.Debug("Client connected", slog.String("name", sesh.User.Name))

	if _, _, err = self.hub.ConnectClient("", client); err != nil {
		logger.Error("Can't connect client", slog.Any("client", client))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for {
		var payload struct {
			Type   string `json:"msgType"`
			Msg    string `json:"msg"`
			ChatId string `json:"chatId"`
		}

		_, p, err := conn.ReadMessage()
		if err != nil {
			logger.Error("Can't read client websocket message", slog.Any("error", err))
			break
		}

		err = json.Unmarshal(p, &payload)
		if err != nil {
			logger.Error("Failed to unmarshal websocket message", slog.Any("error", err))
			continue
		}

		chatId := payload.ChatId

		switch payload.Type {
		case "changeChat":
			cht, prevCht, err := self.hub.ConnectClient(chatId, client)
			if err != nil {
				logger.Error("Failed to connect client", slog.Any("error", err))
				return
			}

			msgs, err := cht.GetMessages()
			if err != nil {
				logger.Error("Change chat: Failed to get messages", slog.Any("error", err))
				return
			}

			ctx := session.ContextWithSessionId(context.Background(), client.SessionId)

			var html bytes.Buffer
			components.ChatWindow(cht.Id, msgs).Render(ctx, &html)
			components.ChatListItem(cht, "active").Render(ctx, &html)
			if prevCht != nil {
				components.ChatListItem(prevCht, "").Render(ctx, &html)
			}

			client.Send(html.Bytes())
		}
	}

	self.hub.RemoveClient(client)
}

func (self *ChatHandler) NewMessage(w http.ResponseWriter, r *http.Request) {
	chatId := r.PathValue("chatId")
	msgContent := r.FormValue("msg")
	sesh := session.GetSession(r.Context())

	msg := internal.New(
		chatId,
		sesh.User.Id,
		msgContent,
		internal.TextMessage,
	)

	cht := self.hub.GetChat(chatId)
	cht.NewMessage(msg, sesh.User.Id)
}

func (self *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), self.logger)

	chatId := r.PathValue("chatId")
	cht := self.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sesh := session.GetSession(r.Context())

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		logger.Error("Can't parse form file", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer file.Close()

	savedFilename, err := self.fileUploader.Upload(fileHeader.Filename, file)
	if err != nil {
		logger.Error("Can't upload file", slog.Any("error", err))
		http.Error(w, "Unexpected error while uploading file", http.StatusInternalServerError)
		return
	}

	msg := internal.New(
		cht.Id,
		sesh.User.Id,
		savedFilename,
		internal.ImageMessage,
	)

	cht.NewMessage(msg, sesh.User.Id)
}

func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), h.logger)
	msgId := r.PathValue("messageId")
	msg, err := h.store.GetMessage(msgId)
	if err != nil {
		logger.Error("Can't get message", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	components.MessageBox(msg, true, false).Render(r.Context(), w)
}

func (h *ChatHandler) GetMessageEdit(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), h.logger)
	msgId := r.PathValue("messageId")
	msg, err := h.store.GetMessage(msgId)
	if err != nil {
		logger.Error("Can't get message", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	components.
		MessageBox(msg, true, true).
		Render(r.Context(), w)
}

func (self *ChatHandler) PostMessageEdit(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), self.logger)
	r.ParseForm()

	chatId := r.PathValue("chatId")
	cht := self.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	msgId := r.PathValue("messageId")
	msgContent := r.FormValue("msgContent")
	err := cht.UpdateMessageContent(msgId, msgContent)
	if err != nil {
		logger.Error("Can't update message's content", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (self *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (self *ChatHandler) MessageHide(hide bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := LoggerFromContext(r.Context(), self.logger)
		chatId := r.PathValue("chatId")
		cht := self.hub.GetChat(chatId)
		if cht == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		sesh := session.GetSession(r.Context())
		msgId := r.PathValue("messageId")
		err := cht.SetHideMessage(msgId, sesh.User.Id, hide)
		if err != nil {
			logger.Error("Failed set message to hidden", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func (self *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context(), self.logger)
	chatId := r.PathValue("chatId")
	cht := self.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	msgId := r.PathValue("messageId")
	err := cht.DeleteMessage(msgId)
	if err != nil {
		logger.Error("Failed to delete message", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (self *ChatHandler) CreateChat(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	chatName := r.FormValue("chatName")
	self.hub.AddChat(chatName)
}

func NewHttpClient(conn *websocket.Conn, sessionId session.SessionId, logger *slog.Logger) *HttpClient {
	return &HttpClient{
		id:        sessionId.String(),
		SessionId: sessionId,
		conn:      conn,
		connMux:   sync.Mutex{},
		logger:    logger,
	}
}

type HttpClient struct {
	id        string
	SessionId session.SessionId

	conn    *websocket.Conn
	connMux sync.Mutex

	logger *slog.Logger
}

func (self *HttpClient) GetId() string { return self.id }

func (self *HttpClient) HandleEvent(evtType internal.EventType, evtData internal.EventData) {
	ctx := session.ContextWithSessionId(context.Background(), self.SessionId)
	var html bytes.Buffer

	switch evtType {
	case internal.Event_NewMessage:

		if evtData.Connected {
			msg := evtData.Msg
			components.
				MessagesList([]*internal.Message{msg}, true).
				Render(ctx, &html)

			children := components.ContextMenu(msg, false)
			ctx = templ.WithChildren(ctx, children)
			components.ContextMenusWrapper(true).Render(ctx, &html)
		} else {
			components.ChatListItem(evtData.Cht, "newMessage").Render(ctx, &html)
		}

	case
		internal.Event_UpdateMessage,
		internal.Event_DeleteMessage,
		internal.Event_EditMessage,
		internal.Event_HideMessage,
		internal.Event_PinMessage:
		if evtData.OnlySender && self.id != evtData.SenderId {
			break
		}

		if evtData.Connected {
			msg := evtData.Msg

			components.
				MessageBox(msg, true, false).
				Render(ctx, &html)

			components.
				ContextMenu(msg, true).
				Render(ctx, &html)
		} else {
			components.ChatListItem(evtData.Cht, "newMessage").Render(ctx, &html)
		}
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
		self.logger.Error("Failed to write message to http client", slog.Any("error", err))
	}
}
