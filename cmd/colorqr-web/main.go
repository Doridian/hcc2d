// colorqr-web serves the HCC2D demo web app (the web/ directory) for local
// development. Build the WebAssembly codec first with "make web".
//
//	go run ./cmd/colorqr-web -addr localhost:8080 -dir web
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	dir := flag.String("dir", "web", "directory to serve")
	flag.Parse()

	if _, err := os.Stat(filepath.Join(*dir, "colorqr.wasm")); err != nil {
		log.Printf("warning: %s/colorqr.wasm is missing; run \"make web\" first", *dir)
	}
	files := http.FileServer(http.Dir(*dir))
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	log.Printf("serving %s on http://%s/", *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
