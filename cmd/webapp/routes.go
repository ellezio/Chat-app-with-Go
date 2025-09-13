package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/session"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
)

func routs() http.Handler {
	mux := http.NewServeMux()

	// TODO: static folder generated assets
	assetsDir := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/js/", assetsDir)
	mux.Handle("/css/", assetsDir)

	// TODO: separe file server
	filesDir := http.FileServer(http.Dir("web/files"))
	mux.Handle("/files/", http.StripPrefix("/files/", filesDir))

	err := store.InitConn()
	if err != nil {
		panic(err)
	}

	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags)

	chatHandler := newChatHandler()

	mux.Handle("/", OnlyLoggedIn(http.HandlerFunc(chatHandler.Homepage)))
	mux.Handle("/chatroom", OnlyLoggedIn(http.HandlerFunc(chatHandler.Chatroom)))
	mux.Handle("/uploadfile", OnlyLoggedIn(http.HandlerFunc(chatHandler.UploadFile)))

	mux.Handle("GET /login", http.HandlerFunc(chatHandler.LoginPage))
	mux.Handle("POST /login", http.HandlerFunc(chatHandler.Login))

	mux.Handle("POST /chat", OnlyLoggedIn(http.HandlerFunc(chatHandler.CreateChat)))

	mux.Handle("GET /message", OnlyLoggedIn(http.HandlerFunc(chatHandler.GetMessage)))
	mux.Handle("GET /message/edit", OnlyLoggedIn(http.HandlerFunc(chatHandler.GetMessageEdit)))
	mux.Handle("POST /message/edit", OnlyLoggedIn(http.HandlerFunc(chatHandler.PostMessageEdit)))
	mux.Handle("POST /message/pin", OnlyLoggedIn(http.HandlerFunc(chatHandler.MessagePin)))
	mux.Handle("POST /message/hide/{doHide}", OnlyLoggedIn(http.HandlerFunc(chatHandler.MessageHide)))
	mux.Handle("POST /message/delete", OnlyLoggedIn(http.HandlerFunc(chatHandler.MessageDelete)))

	mux.HandleFunc("/api/get-sessions", func(w http.ResponseWriter, r *http.Request) {
		seshs := session.GetSessions()
		data, err := json.Marshal(seshs)
		if err != nil {
			fmt.Fprintln(w, "Failed to read sessions.")
			fmt.Fprintf(w, "%v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Write(data)
	})

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
