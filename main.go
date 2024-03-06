package main

import (
	"fmt"
	"html/template"
	"net/http"
)

func main() {
	publicFs := http.FileServer(http.Dir("public"))
	http.Handle("/scripts/", publicFs)
	http.Handle("/styles/", publicFs)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseGlob("app/templates/pages/chat/*.gohtml"))
		template.Must(tmpl.ParseGlob("app/templates/layouts/*.gohtml"))

		tmpl.ExecuteTemplate(w, "index.gohtml", nil)
	})

	http.HandleFunc("/send-msg", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "Error: %s", err)
		}

		fmt.Fprintf(
			w,
			"<li class=\"msgs-list__msg-box msgs-list__msg-box--my\">%s</li>",
			r.PostForm.Get("msg"),
		)
	})

	http.ListenAndServe(":80", nil)
}
