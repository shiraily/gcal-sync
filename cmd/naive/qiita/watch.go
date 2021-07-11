package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Google Calendar APIが扱うtime format
const gcalTimeFormat = "2006-01-02T15:04:05-07:00"

func newChannel(webhookURL string) *calendar.Channel {
	id, _ := uuid.NewRandom() // UUID推奨
	// 1ヶ月以上先を指定しても最大1ヶ月後
	exp, _ := time.Parse(gcalTimeFormat, "2030-01-01T00:00:00+09:00")
	ch := calendar.Channel{
		Id:         id.String(),
		Type:       "webhook",
		Expiration: exp.UnixNano() / int64(time.Millisecond), // Unix timestamp (ミリ秒)
		Address:    webhookURL,
	}
	return &ch
}

func TokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// const ScopeALL = calendar.CalendarEventsScope // 広い
// libraryにないので自分で指定
const scope = "https://www.googleapis.com/auth/calendar.events.owned"

func NewCalendarService(ctx context.Context, tokenFile string) *calendar.Service {
	b, _ := ioutil.ReadFile("credentials.json")
	config, _ := google.ConfigFromJSON(b, scope)
	tok, _ := TokenFromFile(tokenFile)
	svc, _ := calendar.NewService(ctx, option.WithHTTPClient(config.Client(ctx, tok)))
	return svc
}

const serviceAccountKey = "service_account_key.json"

func NewFirestoreClient(ctx context.Context, project string) *firestore.Client {
	app, _ := firebase.NewApp(ctx, &firebase.Config{ProjectID: project}, option.WithCredentialsFile(serviceAccountKey))
	cli, _ := app.Firestore(ctx)
	return cli
}

const CalendarId = "primary"

func Watch() {
	flag.Parse()
	webhookURL := flag.Args()[0]
	ctx := context.Background()

	ch := newChannel(webhookURL)
	svc := NewCalendarService(ctx, "src_token.json")
	res, _ := svc.Events.Watch(CalendarId, ch).Do()

	project := flag.Args()[1] // プロジェクト名
	fsClient := NewFirestoreClient(ctx, project)
	_, err := fsClient.Collection("calendar").Doc("channel").Set(ctx, map[string]interface{}{
		"channelId":  ch.Id,
		"resourceId": res.ResourceId,
		"exp":        res.Expiration, // デバッグ用
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("channelId=%s, resourceId=%s, exp=%d", ch.Id, res.ResourceId, res.Expiration)
}
