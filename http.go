package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func ListenHTTP(bind string, buffer *MessageBuffer) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if stats, err := json.Marshal(buffer.Stats()); err == nil {
			fmt.Fprintf(w, "%s\n", stats)
		} else {
			log.Printf("error serializing buffer stats: %s\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "{}\n")
		}
	})
	log.Printf("listening: %s\n", bind)
	http.ListenAndServe(bind, nil)
}
