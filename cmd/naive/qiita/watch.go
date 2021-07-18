package main

import (
	"context"
	"flag"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/google/uuid"
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

const serviceAccountKey = "service_account_key.json"

func NewCalendarService(ctx context.Context) *calendar.Service {
	svc, _ := calendar.NewService(ctx, option.WithCredentialsFile(serviceAccountKey))
	return svc
}

func NewFirestoreClient(ctx context.Context, project string) *firestore.Client {
	app, _ := firebase.NewApp(ctx, &firebase.Config{ProjectID: project}, option.WithCredentialsFile(serviceAccountKey))
	cli, _ := app.Firestore(ctx)
	return cli
}

func Watch() {
	flag.Parse()
	webhookURL := flag.Args()[0]
	ctx := context.Background()

	ch := newChannel(webhookURL)
	svc := NewCalendarService(ctx)
	calendarId := flag.Args()[1]
	res, _ := svc.Events.Watch(calendarId, ch).Do()

	project := flag.Args()[2] // プロジェクト名
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
