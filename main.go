package main

import (
	"chatting-app/components"
	"chatting-app/handlers"
	"fmt"
	"net/http"
)

func main() {
	var msgs []components.Message

	publicFs := http.FileServer(http.Dir("public"))
	http.Handle("/scripts/", publicFs)
	http.Handle("/styles/", publicFs)

	chatHandler := handlers.NewChatHandler(&msgs)

	http.HandleFunc("/", chatHandler.Page)
	http.HandleFunc("/send-msg", chatHandler.SendMessage)
	http.HandleFunc("/login", chatHandler.Login)

	fmt.Println("Start listening on :3000")

	if err := http.ListenAndServe(":3000", nil); err != nil {
		fmt.Println(err)
	}
}
