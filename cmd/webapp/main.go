package main

import (
	"fmt"
	"net/http"

	"github.com/pawellendzion/Chat-app-with-Go/internal/database"
	"github.com/pawellendzion/Chat-app-with-Go/internal/handlers"
	"github.com/pawellendzion/Chat-app-with-Go/internal/services"
)

func main() {
	db := database.NewDB()

	publicFs := http.FileServer(http.Dir("web/assets"))
	http.Handle("/js/", publicFs)
	http.Handle("/css/", publicFs)

	chatService := services.NewChatService(db)
	chatHandler := handlers.NewChatHandler(*chatService)

	http.HandleFunc("/", chatHandler.Page)
	http.HandleFunc("/chatroom", chatHandler.Chatroom)
	http.HandleFunc("/login", chatHandler.Login)

	fmt.Println("Start listening on :3000")
	if err := http.ListenAndServe(":3000", nil); err != nil {
		fmt.Println(err)
	}
}
