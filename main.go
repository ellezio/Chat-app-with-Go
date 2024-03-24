package main

import (
	"chatting-app/components"
	"context"
	"fmt"
	"net/http"
)

func main() {
	var msgs []components.Message

	publicFs := http.FileServer(http.Dir("public"))
	http.Handle("/scripts/", publicFs)
	http.Handle("/styles/", publicFs)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		username := ""
		if cookie, err := r.Cookie("username"); err == nil {
			username = cookie.Value
		}

		ctx := context.WithValue(r.Context(), "username", username)
		components.Page(msgs).Render(ctx, w)
	})

	http.HandleFunc("/send-msg", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("username"); err == nil {
			if err := r.ParseForm(); err != nil {
				fmt.Fprintf(w, "Error: %s", err)
			}

			msg := components.Message{
				Author:  cookie.Value,
				Content: r.PostForm.Get("msg"),
			}
			msgs = append(msgs, msg)

			ctx := context.WithValue(r.Context(), "username", cookie.Value)
			components.MessageBox(msg).Render(ctx, w)
		}
	})

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
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
	})

	fmt.Println("Start listening on :3000")
	err := http.ListenAndServe(":3000", nil)
	if err != nil {
		fmt.Println(err)
	}
}
