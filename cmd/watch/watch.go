package main

import (
	"fmt"
	"log"

	"github.com/shiraily/gcal-sync/calendar"
)

func main() {
	cli := calendar.NewClient()
	defer cli.Close()
	calId, err := cli.StartWatch()
	if err != nil {
		log.Fatalf("Start watch: %s", err)
	}
	fmt.Printf("Watch id=%s", calId)
}
