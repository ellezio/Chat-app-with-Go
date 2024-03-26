package handlers

import (
	"chatting-app/components"
	"context"
	"fmt"
	"net/http"
)

type ChatHandler struct {
	msgs *[]components.Message
}

func NewChatHandler(msgs *[]components.Message) *ChatHandler {
	return &ChatHandler{
		msgs: msgs,
	}
}

func (h *ChatHandler) Page(w http.ResponseWriter, r *http.Request) {
	username := ""
	if cookie, err := r.Cookie("username"); err == nil {
		username = cookie.Value
	}

	ctx := context.WithValue(r.Context(), "username", username)
	components.Page(*h.msgs).Render(ctx, w)
}

func (h *ChatHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fmt.Println(err)
	}

	cookie := http.Cookie{
		Name:   "username",
		Value:  r.PostForm.Get("username"),
		Path:   "/",
		MaxAge: 3600,
	}

	http.SetCookie(w, &cookie)
}

func (h *ChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("username"); err == nil {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "Error: %s", err)
		}

		msg := components.Message{
			Author:  cookie.Value,
			Content: r.PostForm.Get("msg"),
		}
		*h.msgs = append(*h.msgs, msg)

		ctx := context.WithValue(r.Context(), "username", cookie.Value)
		components.MessageBox(msg).Render(ctx, w)
	}
}
