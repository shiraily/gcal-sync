package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/shiraily/gcal-sync/calendar"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		fmt.Printf("Require only 2 arg: %d", len(args))
		return
	}
	channelId := args[0]
	resourceId := args[1]

	cli := calendar.NewClient()
	defer cli.Close()
	chId, err := cli.StopWatch(channelId, resourceId)
	if err != nil {
		log.Fatalf("Stop watch: %s", err)
	}
	fmt.Printf("Stop channel=%s", chId)
}
