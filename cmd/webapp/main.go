package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/config"
	"github.com/ellezio/Chat-app-with-Go/internal/log"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
	"github.com/ellezio/Chat-app-with-Go/web/components"
)

func setupMux(chatHandler *ChatHandler) http.Handler {
	mux := http.NewServeMux()

	assetsDir := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/js/", assetsDir)
	mux.Handle("/css/", assetsDir)

	mux.HandleFunc("GET /login", handleError(chatHandler.LoginPage))
	mux.HandleFunc("POST /login", handleError(chatHandler.Login))
	mux.HandleFunc("GET /register", handleError(chatHandler.RegisterPage))
	mux.HandleFunc("POST /register", handleError(chatHandler.Register))

	loginMux := http.NewServeMux()
	loginMux.HandleFunc("/", handleError(chatHandler.Homepage))
	loginMux.HandleFunc("GET /chatroom", handleError(chatHandler.Chatroom))
	loginMux.HandleFunc("POST /chats", handleError(chatHandler.CreateChat))
	loginMux.HandleFunc("POST /chats/{chatId}/uploadfile", handleError(chatHandler.UploadFile))
	loginMux.HandleFunc("GET /chats/{chatId}/messages/{messageId}", handleError(chatHandler.GetMessage))
	loginMux.HandleFunc("GET /chats/{chatId}/messages/{messageId}/edit", handleError(chatHandler.GetMessageEdit))
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/edit", handleError(chatHandler.PostMessageEdit))
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/pin", handleError(chatHandler.MessagePin))
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/hide", handleError(chatHandler.MessageHide(true)))
	loginMux.HandleFunc("PUT /chats/{chatId}/messages/{messageId}/show", handleError(chatHandler.MessageHide(false)))
	loginMux.HandleFunc("DELETE /chats/{chatId}/messages/{messageId}", handleError(chatHandler.MessageDelete))
	loginMux.HandleFunc("POST /chats/{chatId}/messages", handleError(chatHandler.NewMessage))
	mux.Handle("/", AuthMiddleware(loginMux))

	return session.Middleware(mux)
}

func handleError(h func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if re := recover(); re != nil {
				var err error
				switch re := re.(type) {
				case error:
					err = re
				default:
					err = fmt.Errorf("%v", re)
				}
				log.Ctx(r.Context()).Error("Panic occurred", slog.Any("error", err))
				renderError(w, r, err)
			}
		}()

		err := h(w, r)
		if err != nil {
			log.Ctx(r.Context()).Error("Handler returned error", slog.Any("error", err))
			renderError(w, r, err)
		}
	}
}

func renderError(w http.ResponseWriter, r *http.Request, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	if r.Header.Get("Hx-Request") == "true" {
		if err := components.ErrorPopup(err.Error()).Render(r.Context(), w); err != nil {
			log.Ctx(r.Context()).Error("failed to render error popup", slog.Any("error", err))
		}
	} else {
		if err := components.ErrorPage(err.Error()).Render(r.Context(), w); err != nil {
			log.Ctx(r.Context()).Error("", slog.Any("failed to render error page", err))
		}
	}
}

func AuthMiddleware(next http.Handler) http.Handler {
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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With("servie", "webapp")
	log.DefaultContextLogger = logger

	host := flag.String("host", "", "")
	port := flag.String("port", "3000", "")
	fileHost := flag.String("file-host", "localhost", "file server host")
	fielPort := flag.String("file-port", "3001", "file server port")
	flag.Parse()

	cfg := readConfig()

	err := store.InitConn(cfg.MongoDB, cfg.Redis)
	if err != nil {
		panic(err)
	}
	sto := &store.MongodbStore{}

	fileUploader := NewFileUploader(*fileHost, *fielPort)
	chatHandler, hub := newChatHandler(sto, fileUploader)
	err = hub.Start(cfg.RabbitMQ)
	if err != nil {
		panic(err)
	}

	mux := setupMux(chatHandler)

	addr := net.JoinHostPort(*host, *port)
	logger.Info(fmt.Sprintf("Listening at %s", addr))
	err = http.ListenAndServe(addr, log.Middleware(mux, logger))
	if err != nil {
		logger.Error("Server stop listening", slog.Any("error", err))
	}
}
