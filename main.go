package main

import (
	"fmt"
	"html/template"
	"net/http"
)

type Message struct {
	Author string
	Msg    string
}

func main() {
	var msgs []Message

	publicFs := http.FileServer(http.Dir("public"))
	http.Handle("/scripts/", publicFs)
	http.Handle("/styles/", publicFs)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		type MessagePageData struct {
			Message
			IsMyMsg bool
		}

		var pageData struct {
			IsLoggedIn bool
			Msgs       []MessagePageData
		}

		cookie, err := r.Cookie("username")
		pageData.IsLoggedIn = err == nil

		var username string
		if err == nil {
			username = cookie.Value
		}

		pageData.Msgs = make([]MessagePageData, len(msgs))
		for _, msg := range msgs {
			pageData.Msgs = append(pageData.Msgs, MessagePageData{
				Message: msg,
				IsMyMsg: msg.Author == username,
			})
		}

		tmpl := template.Must(template.ParseGlob("app/templates/pages/chat/*.tmpl"))
		template.Must(tmpl.ParseGlob("app/templates/layouts/*.tmpl"))
		tmpl.ExecuteTemplate(w, "index.tmpl", pageData)
	})

	http.HandleFunc("/send-msg", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "Error: %s", err)
		}

		cookie, _ := r.Cookie("username")
		msgs = append(msgs, Message{cookie.Value, r.PostForm.Get("msg")})

		fmt.Fprintf(
			w,
			"<li class=\"msgs-list__msg-box msgs-list__msg-box--right\">%s</li>",
			r.PostForm.Get("msg"),
		)
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

	http.ListenAndServe(":80", nil)
}
