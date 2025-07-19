package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ellezio/Chat-app-with-Go/internal/database"
	"github.com/ellezio/Chat-app-with-Go/internal/services"
	"github.com/ellezio/Chat-app-with-Go/internal/session"
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

	db := database.NewDB()

	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags)

	chatService := services.NewChatService(db)
	chatHandler := newChatHandler(*chatService)

	mux.HandleFunc("/", chatHandler.Page)
	mux.HandleFunc("/chatroom", chatHandler.Chatroom)
	mux.HandleFunc("/login", chatHandler.Login)
	mux.HandleFunc("/uploadfile", chatHandler.UploadFile)

	mux.HandleFunc("/api/get-sessions", func(w http.ResponseWriter, r *http.Request) {
		seshs := session.GetSessions()
		data, err := json.Marshal(seshs)
		if err != nil {
			fmt.Fprintln(w, "Failed to read sessions.")
			fmt.Fprintf(w, "%v\n", err)
			w.WriteHeader(500)
			return
		}

		w.Write(data)
	})

	return session.Middleware(mux)
}
