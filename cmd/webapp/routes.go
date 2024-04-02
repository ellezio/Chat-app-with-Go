package main

import (
	"net/http"

	"github.com/pawellendzion/Chat-app-with-Go/internal/database"
	"github.com/pawellendzion/Chat-app-with-Go/internal/services"
)

func routs() http.Handler {
	mux := http.NewServeMux()

	publicFs := http.FileServer(http.Dir("web/assets"))
	mux.Handle("/js/", publicFs)
	mux.Handle("/css/", publicFs)

	db := database.NewDB()
	chatService := services.NewChatService(db)
	chatHandler := newChatHandler(*chatService)

	mux.HandleFunc("/", chatHandler.Page)
	mux.HandleFunc("/chatroom", chatHandler.Chatroom)
	mux.HandleFunc("/login", chatHandler.Login)

	return mux
}
