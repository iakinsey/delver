package main

import (
	"log"
	"net/http"
)

func main() {
	log.Print("example site: http://localhost:8088")
	log.Fatal(http.ListenAndServe("127.0.0.1:8088", http.FileServer(http.Dir("examples/site/pages"))))
}
