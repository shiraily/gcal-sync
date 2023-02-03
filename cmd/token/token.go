package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	oauthToken "github.com/shiraily/gcal-sync/oauth"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config, filePrefix string) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := fmt.Sprintf("%s_token.json", filePrefix)
	tok, err := oauthToken.TokenFromFile(tokFile)
	if err != nil {
		tok = oauthToken.GetTokenFromWeb(config)
		oauthToken.SaveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		return
	}
	filePrefix := args[0]
	ctx := context.Background()
	b, err := ioutil.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, "https://www.googleapis.com/auth/calendar.events.owned")
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config, filePrefix)

	srv, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	t := time.Now().Format(time.RFC3339)
	events, err := srv.Events.List("primary").ShowDeleted(false).
		SingleEvents(false).TimeMin(t).MaxResults(10).OrderBy("startTime").Do()
	if err != nil {
		log.Fatalf("Unable to retrieve next ten of the user's events: %v", err)
	}
	fmt.Println("Upcoming events:")
	if len(events.Items) == 0 {
		fmt.Println("No upcoming events found.")
	} else {
		for _, item := range events.Items {
			date := item.Start.DateTime
			if date == "" {
				date = item.Start.Date
			}
			fmt.Printf("%v (%v)\n", item.Summary, date)
		}
	}
}
