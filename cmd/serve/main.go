// Command serve runs a simple HTTP file server for local development.
// It serves the contents of public/ on port 8080.
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	dir := "public"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		log.Fatalf("serve: directory %q not found — run `make build` first", dir)
	}

	addr := ":8080"
	log.Printf("Serving %s at http://localhost%s", dir, addr)

	fs := http.FileServer(http.Dir(dir))
	if err := http.ListenAndServe(addr, fs); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
