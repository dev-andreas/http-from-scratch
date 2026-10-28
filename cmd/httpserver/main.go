package main

import (
	"fmt"
	"httpServer/internal/http1"
	"net/http"
)

const addr = ":8080"

func main() {
	http1.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello")
	})
	if err := http1.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}
