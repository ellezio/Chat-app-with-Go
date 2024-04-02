package main

import (
	"fmt"
	"net/http"
)

func main() {
	srv := &http.Server{
		Addr:    ":3000",
		Handler: routs(),
	}

	fmt.Println("Start listening on :3000")
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println(err)
	}
}
