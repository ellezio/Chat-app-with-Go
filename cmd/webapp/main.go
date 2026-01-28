package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/config"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
)

func setupMux(chatHandler *ChatHandler) http.Handler {
	mux := http.NewServeMux()

	assetsDir := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/js/", assetsDir)
	mux.Handle("/css/", assetsDir)

	mux.HandleFunc("GET /login", chatHandler.LoginPage)
	mux.HandleFunc("POST /login", chatHandler.Login)
	mux.HandleFunc("GET /register", chatHandler.RegisterPage)
	mux.HandleFunc("POST /register", chatHandler.Register)

	loginMux := http.NewServeMux()
	loginMux.HandleFunc("/", chatHandler.Homepage)
	loginMux.HandleFunc("GET /chatroom", chatHandler.Chatroom)
	loginMux.HandleFunc("POST /chats", chatHandler.CreateChat)
	loginMux.HandleFunc("POST /chats/{chatId}/uploadfile", chatHandler.UploadFile)
	loginMux.HandleFunc("GET /chats/{chatId}/messages/{messageId}", chatHandler.GetMessage)
	loginMux.HandleFunc("GET /chats/{chatId}/messages/{messageId}/edit", chatHandler.GetMessageEdit)
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/edit", chatHandler.PostMessageEdit)
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/pin", chatHandler.MessagePin)
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/hide", chatHandler.MessageHide(true))
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/show", chatHandler.MessageHide(false))
	loginMux.HandleFunc("DELETE /chats/{chatId}/messages/{messageId}", chatHandler.MessageDelete)
	loginMux.HandleFunc("POST /chats/{chatId}/messages", chatHandler.NewMessage)
	mux.Handle("/", OnlyLoggedIn(loginMux))

	return session.Middleware(mux)
}

func OnlyLoggedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !session.IsLoggedIn(ctx) {
			if r.Header.Get("Hx-Request") == "true" {
				w.Header().Add("Hx-Redirect", "/login")
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func readConfig() config.Configuration {
	b, err := os.ReadFile("config.json")
	if err != nil {
		panic(err)
	}

	var cfg config.Configuration
	err = json.Unmarshal(b, &cfg)
	if err != nil {
		panic(err)
	}

	return cfg
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := ":3000"
	cfg := readConfig()

	err := store.InitConn(cfg.MongoDB, cfg.Redis)
	if err != nil {
		panic(err)
	}
	sto := &store.MongodbStore{}

	chatHandler, hub := newChatHandler(sto, logger)
	err = hub.Start(cfg.RabbitMQ)
	if err != nil {
		panic(err)
	}

	mux := setupMux(chatHandler)
	http.ListenAndServe(addr, mux)
}
