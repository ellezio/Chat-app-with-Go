package main

import (
	"fmt"
	"html/template"
	"net/http"
)

type Messages struct {
	Author string
	Msg    string
}

func main() {
	var msgs []Messages
	msgs = append(msgs, Messages{"Jone", "Hi there"})

	publicFs := http.FileServer(http.Dir("public"))
	http.Handle("/scripts/", publicFs)
	http.Handle("/styles/", publicFs)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseGlob("app/templates/pages/chat/*.tmpl"))
		template.Must(tmpl.ParseGlob("app/templates/layouts/*.tmpl"))

		tmpl.ExecuteTemplate(w, "index.tmpl", msgs)
	})

	http.HandleFunc("/send-msg", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "Error: %s", err)
		}

		msgs = append(msgs, Messages{"Me", r.PostForm.Get("msg")})

		fmt.Fprintf(
			w,
			"<li class=\"msgs-list__msg-box msgs-list__msg-box--right\">%s</li>",
			r.PostForm.Get("msg"),
		)
	})

	http.ListenAndServe(":80", nil)
}
