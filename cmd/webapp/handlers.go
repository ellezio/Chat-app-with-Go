package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
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
	upgrader websocket.Upgrader
	hub      *internal.Hub
}

var sto = &store.MongodbStore{}

func newChatHandler() (*ChatHandler, *internal.Hub) {
	h := &ChatHandler{
		upgrader: websocket.Upgrader{},
		hub:      internal.NewHub(sto),
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

	user, err := sto.GetUser(username)
	if err != nil {
		// TODO: move it to some registration
		user = &internal.User{Name: username}
		if err = sto.CreateUser(user); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
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

func (self *ChatHandler) Chatroom(w http.ResponseWriter, r *http.Request) {
	conn, err := self.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	sesh := session.GetSession(r.Context())
	client := NewHttpClient(conn, sesh.Id)

	log.Printf("%s Connected\r\n", sesh.User.Name)

	if _, _, err = self.hub.ConnectClient("", client); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Println(err)
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
			log.Println(err)
			break
		}

		err = json.Unmarshal(p, &payload)
		if err != nil {
			log.Println(err)
			continue
		}

		chatId := payload.ChatId

		switch payload.Type {
		case "changeChat":
			cht, prevCht, err := self.hub.ConnectClient(chatId, client)
			if err != nil {
				log.Printf("Failed to connect client. %v\n", err)
				return
			}

			msgs, err := cht.GetMessages()
			if err != nil {
				log.Printf("Change chat: Failed to get messages. %v\n", err)
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
	err := r.ParseMultipartForm(1024)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	chatId := r.PathValue("chatId")
	cht := self.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sesh := session.GetSession(r.Context())

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
		cht.Id,
		sesh.User.Id,
		fileHeader.Filename,
		internal.ImageMessage,
	)

	cht.NewMessage(msg, sesh.User.Id)
}

func (h *ChatHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	msgId := r.PathValue("messageId")
	msg, err := sto.GetMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	components.MessageBox(msg, true, false).Render(r.Context(), w)
}

func (h *ChatHandler) GetMessageEdit(w http.ResponseWriter, r *http.Request) {
	msgId := r.PathValue("messageId")
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
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (self *ChatHandler) MessagePin(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (self *ChatHandler) MessageHide(hide bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func (self *ChatHandler) MessageDelete(w http.ResponseWriter, r *http.Request) {
	chatId := r.PathValue("chatId")
	cht := self.hub.GetChat(chatId)
	if cht == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	msgId := r.PathValue("messageId")
	err := cht.DeleteMessage(msgId)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (self *ChatHandler) CreateChat(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	chatName := r.FormValue("chatName")
	self.hub.AddChat(chatName)
}

func NewHttpClient(conn *websocket.Conn, sessionId session.SessionId) *HttpClient {
	return &HttpClient{
		id:        sessionId.String(),
		SessionId: sessionId,
		conn:      conn,
		connMux:   sync.Mutex{},
	}
}

type HttpClient struct {
	id        string
	SessionId session.SessionId

	conn    *websocket.Conn
	connMux sync.Mutex
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
		log.Println(err)
	}
}
