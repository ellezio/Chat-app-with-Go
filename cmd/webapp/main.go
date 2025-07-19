package main

import (
	"fmt"
	"net/http"
)

func main() {
	addr := ":3000"

	srv := &http.Server{
		Addr:    addr,
		Handler: routs(),
	}

	fmt.Printf("Start listening on %s\n", addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println(err)
	}
}
