package main

import (
	"log"
	"net/http"
	"os"

	"github.com/shiraily/gcal-sync/calendar"
)

func main() {
	http.HandleFunc("/notify", OnNotify)
	http.HandleFunc("/renew", OnRenew)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func OnNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := calendar.NewClient()
	defer cli.Close()
	log.Printf("channelId=%s, resourceId=%s", r.Header["X-Goog-Channel-Id"], r.Header["X-Goog-Resource-Id"])
	calId, err := cli.Sync(len(r.Header["X-Goog-Resource-State"]) > 0 && r.Header["X-Goog-Resource-State"][0] == "exists")
	if err != nil {
		log.Printf("debug error: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(calId)); err != nil {
		log.Fatal(err)
		return
	}
}

func OnRenew(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := calendar.NewClient()
	defer cli.Close()
	channelId, err := cli.RenewWatch()
	if err != nil {
		log.Fatalf("Renew watch: %s", err)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(channelId)); err != nil {
		log.Fatal(err)
	}
}
