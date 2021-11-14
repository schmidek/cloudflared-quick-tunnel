package main

import (
	"io"
	"net/http"
)

func main() {
	http.HandleFunc("/ping", PingServer)
	http.HandleFunc("/callback", CallbackServer)
	http.ListenAndServe(":8080", nil)
}

func PingServer(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func CallbackServer(w http.ResponseWriter, r *http.Request) {
	if b, err := io.ReadAll(r.Body); err == nil {
		url := string(b) // url you can access the server at, you would send or save this as needed
		println(url)
	}
	w.Write([]byte("success"))
}
