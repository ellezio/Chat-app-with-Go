package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/a-h/templ"
	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/log"
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
	fileUploader FileUploader
	hub          *internal.Hub
	store        internal.Store
}

func newChatHandler(store internal.Store) (*ChatHandler, *internal.Hub) {
	h := &ChatHandler{
		hub:   internal.NewHub(store),
		store: store,
	}

	return h, h.hub
}

func (h *ChatHandler) Homepage(w http.ResponseWriter, r *http.Request) error {
	chts := h.hub.GetChats()

	var bb bytes.Buffer
	components.Homepage(chts, nil).Render(r.Context(), &bb)
	bb.WriteTo(w)
	return nil
}

func (h *ChatHandler) LoginPage(w http.ResponseWriter, r *http.Request) error {
	if session.IsLoggedIn(r.Context()) {
		http.Redirect(w, r, "/", 302)
		return nil
	}

	var bb bytes.Buffer
	components.LoginPage().Render(r.Context(), &bb)
	bb.WriteTo(w)
	return nil
}

func (h *ChatHandler) RegisterPage(w http.ResponseWriter, r *http.Request) error {
	if session.IsLoggedIn(r.Context()) {
		http.Redirect(w, r, "/", 302)
	}

	var bb bytes.Buffer
	components.RegisterPage().Render(r.Context(), &bb)
	bb.WriteTo(w)
	return nil
}

func (h *ChatHandler) Login(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errors.Join(errors.New("Failed to parse login form"), err)
	}

	var bb bytes.Buffer
	username := r.PostForm.Get("username")
	if username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), &bb)
	}

	password := r.PostForm.Get("password")
	if password == "" {
		components.ErrorMsg("password", "Fill the field").Render(r.Context(), &bb)
	}

	if username == "" || password == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		bb.WriteTo(w)
		return nil
	}

	user, err := h.store.GetUser(username)
	if err != nil || !user.CheckPass(password) {
		w.WriteHeader(http.StatusUnauthorized)
		components.ErrorMsg("login", "username or password is invalid").Render(r.Context(), &bb)
		bb.WriteTo(w)
		return nil
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
	return nil
}

func (h *ChatHandler) Register(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errors.Join(errors.New("failed to parse register form"), err)
	}

	var bb bytes.Buffer
	username := r.PostForm.Get("username")
	if username == "" {
		components.ErrorMsg("username", "Fill the field").Render(r.Context(), &bb)
	}

	password := r.PostForm.Get("password")
	if password == "" {
		components.ErrorMsg("password", "Fill the field").Render(r.Context(), &bb)
	}

	if username == "" || password == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		bb.WriteTo(w)
		return nil
	}

	user, err := h.store.GetUser(username)
	if err != nil && !errors.Is(err, store.ErrNoRecord) {
		return errors.Join(errors.New("can't find user"), err)
	} else if user != nil {
		components.ErrorMsg("register", "user already exists").Render(r.Context(), &bb)
		w.WriteHeader(http.StatusUnauthorized)
		bb.WriteTo(w)
		return nil
	}

	user = internal.NewUser(username, password)
	if err := h.store.CreateUser(user); err != nil {
		return errors.Join(errors.New("can't create user"), err)
	}

	w.Header().Add("Hx-Redirect", "/login")
	return nil
}
func (h *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) error {
	logger := log.Ctx(r.Context())

	// TODO: proper check
	h.upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return errors.Join(errors.New("failed to upgrade connection to websocket"), err)
	}
	defer conn.Close()

	sesh := session.GetSession(r.Context())
	client := NewHttpClient(conn, sesh.Id, logger)

	logger.Debug("Client connected", slog.String("name", sesh.User.Name))

	if _, _, err = h.hub.ConnectClient("", client); err != nil {
		// NOTE:
		// this is example of problem with error handling with middleware when we want to log additional info to error.
		// Either we can return some error struct/map or just two logs in same context
		// I will go with two logs, it's easy to track due to trace id.
		logger.Error("Can't connect client", slog.Any("client", client))
		return err
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
			cht, prevCht, err := h.hub.ConnectClient(chatId, client)
			if err != nil {
				logger.Error("Failed to connect client", slog.Any("error", err))
				break
			}

			msgs, err := cht.GetMessages()
			if err != nil {
				logger.Error("Change chat: Failed to get messages", slog.Any("error", err))
				break
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

	h.hub.RemoveClient(client)
	return nil
}

func (h *ChatHandler) NewMessage(w http.ResponseWriter, r *http.Request) error {
	chatId := r.PathValue("chatId")
	msgContent := r.FormValue("msg")
	sesh := session.GetSession(r.Context())

	msg := internal.New(
		chatId,
		sesh.User.Id,
		msgContent,
		internal.TextMessage,
	)

	cht := h.hub.GetChat(chatId)
	cht.NewMessage(msg, sesh.User.Id)
	return nil
}

func (h *ChatHandler) UploadFile(w http.ResponseWriter, r *http.Request) error {
	logger := log.Ctx(r.Context())

	chatId := r.PathValue("chatId")
	cht := h.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	sesh := session.GetSession(r.Context())

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		logger.Error("Can't parse form file", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	defer file.Close()

	savedFilename, err := h.fileUploader.Upload(r.Context(), fileHeader.Filename, file)
	if err != nil {
		return errors.Join(errors.New("can't upload file"), err)
	}

	msg := internal.New(
		cht.Id,
		sesh.User.Id,
		savedFilename,
		internal.ImageMessage,
	)

	cht.NewMessage(msg, sesh.User.Id)
	return nil
}

func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) error {
	msgId := r.PathValue("messageId")
	msg, err := h.store.GetMessage(msgId)
	if err != nil {
		return errors.Join(errors.New("can't get message"), err)
	}

	var bb bytes.Buffer
	components.MessageBox(msg, true, false).Render(r.Context(), &bb)
	bb.WriteTo(w)
	return nil
}

func (h *ChatHandler) GetMessageEdit(w http.ResponseWriter, r *http.Request) error {
	msgId := r.PathValue("messageId")
	msg, err := h.store.GetMessage(msgId)
	if err != nil {
		return errors.Join(errors.New("can't get message"), err)
	}

	var bb bytes.Buffer
	components.MessageBox(msg, true, true).Render(r.Context(), &bb)
	bb.WriteTo(w)
	return nil
}

func (h *ChatHandler) PostMessageEdit(w http.ResponseWriter, r *http.Request) error {
	r.ParseForm()

	chatId := r.PathValue("chatId")
	cht := h.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	msgId := r.PathValue("messageId")
	msgContent := r.FormValue("msgContent")
	if err := cht.UpdateMessageContent(msgId, msgContent); err != nil {
		return errors.Join(errors.New("Can't update message's content"), err)
	}
	return nil
}

func (h *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (h *ChatHandler) MessageHide(hide bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		chatId := r.PathValue("chatId")
		cht := h.hub.GetChat(chatId)
		if cht == nil {
			w.WriteHeader(http.StatusNotFound)
			return nil
		}

		sesh := session.GetSession(r.Context())
		msgId := r.PathValue("messageId")
		err := cht.SetHideMessage(msgId, sesh.User.Id, hide)
		if err != nil {
			return errors.Join(errors.New("Failed set message to hidden"), err)
		}
		return nil
	}
}

func (h *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) error {
	chatId := r.PathValue("chatId")
	cht := h.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	msgId := r.PathValue("messageId")
	err := cht.DeleteMessage(msgId)
	if err != nil {
		errors.Join(errors.New("Failed to delete message"), err)
	}
	return nil
}

func (h *ChatHandler) CreateChat(w http.ResponseWriter, r *http.Request) error {
	r.ParseForm()
	chatName := r.FormValue("chatName")
	h.hub.AddChat(chatName)
	return nil
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

func (c *HttpClient) GetId() string { return c.id }

func (c *HttpClient) HandleEvent(evtType internal.EventType, evtData internal.EventData) {
	ctx := session.ContextWithSessionId(context.Background(), c.SessionId)
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
		if evtData.OnlySender && c.id != evtData.SenderId {
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

	c.Send(html.Bytes())
}

func (c *HttpClient) Send(data []byte) {
	c.connMux.Lock()
	defer c.connMux.Unlock()

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Error("Failed to write message to http client", slog.Any("error", err))
	}
}
