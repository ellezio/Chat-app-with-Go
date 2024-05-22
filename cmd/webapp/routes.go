package main

import (
	"log"
	"net/http"
	"os"

	"github.com/pawellendzion/Chat-app-with-Go/internal/database"
	"github.com/pawellendzion/Chat-app-with-Go/internal/services"
)

func routs() http.Handler {
	mux := http.NewServeMux()

	assetsDir := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/js/", assetsDir)
	mux.Handle("/css/", assetsDir)

	filesDir := http.FileServer(http.Dir("web/files"))
	mux.Handle("/files/", http.StripPrefix("/files/", filesDir))

	db := database.NewDB()
	logger := log.New(os.Stdout, "INFO:", log.LstdFlags)

	chatService := services.NewChatService(db)
	chatHandler := newChatHandler(logger, *chatService)

	mux.HandleFunc("/", chatHandler.Page)
	mux.HandleFunc("/chatroom", chatHandler.Chatroom)
	mux.HandleFunc("/login", chatHandler.Login)
	mux.HandleFunc("/uploadfile", chatHandler.UploadFile)

	return mux
}
