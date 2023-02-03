package calendar

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/google/uuid"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/shiraily/gcal-sync/config"
	"github.com/shiraily/gcal-sync/oauth"
)

const (
	gcalTimeFormat = "2006-01-02T15:04:05-07:00"

	// for oauth
	calendarScopeWithOAuth = "https://www.googleapis.com/auth/calendar.events.owned"
	// oauthClientSecret      = redentials.json"
	// calendarId             = "primary" // if use OAuth client, calendar id is always primary
)

var (
	serviceAccountClientSecret = "service_account_key.json"
)

type Client struct {
	ctx   context.Context
	conf  *config.Config
	svc   *calendar.Service
	fsCli *firestore.Client
}

func NewClient() Client {
	cli := &Client{}
	// envs
	cli.conf = config.GetConfig()
	cli.ctx = context.Background()

	srcSrv, err := NewCalendarServiceWithServiceAccount(cli.ctx, serviceAccountClientSecret)
	if err != nil {
		log.Fatalf("Retrieve Calendar client: %s", err)
	}
	cli.svc = srcSrv

	cli.fsCli, err = cli.NewFirestoreApp(serviceAccountClientSecret)
	if err != nil {
		log.Fatalf("Create firestore cli: %s", err)
	}

	return *cli
}

func NewCalendarService(ctx context.Context, credentialFile, oauthTokenFile string) (*calendar.Service, error) {
	b, err := ioutil.ReadFile(credentialFile)
	if err != nil {
		return nil, fmt.Errorf("read client secret file: %s", err)
	}
	config, err := google.ConfigFromJSON(b, calendarScopeWithOAuth)
	if err != nil {
		return nil, fmt.Errorf("parse client secret file to config: %s", err)
	}

	tok, err := oauth.TokenFromFile(oauthTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read OAuth token: %s", err)
	}
	return calendar.NewService(ctx, option.WithHTTPClient(config.Client(ctx, tok)))
}

func (cli *Client) NewFirestoreApp(credentialFile string) (*firestore.Client, error) {
	app, err := firebase.NewApp(cli.ctx, &firebase.Config{ProjectID: cli.conf.Project}, option.WithCredentialsFile(credentialFile))
	if err != nil {
		return nil, err
	}
	return app.Firestore(cli.ctx)
}

func (cli *Client) Close() {
	cli.fsCli.Close()
}

func (cli *Client) SyncInitial() error {
	t := time.Now().Format(time.RFC3339)
	events, err := cli.svc.Events.List(cli.conf.SrcCalId).ShowDeleted(false).
		SingleEvents(true).TimeMin(t).Do()
	if err != nil {
		return fmt.Errorf("get first token: %s", err)
	}
	if err := cli.saveToken(events.NextSyncToken); err != nil {
		return err
	}
	log.Printf("Initial full sync got token: %s", events.NextSyncToken)
	return nil
}

func (cli *Client) Sync() error {
	nextToken, err := cli.readToken()
	if err != nil {
		return err
	}
	if nextToken == "" {
		return errors.New("nextSyncToken is empty")
	}

	log.Printf("use token: %s", nextToken)
	events, err := cli.svc.Events.List(cli.conf.SrcCalId).SyncToken(nextToken).Do()
	if err != nil {
		return fmt.Errorf("retrieve next events: %s", err)
	}

	if err := cli.saveToken(events.NextSyncToken); err != nil {
		return err
	}

	if len(events.Items) == 0 {
		log.Println("No upcoming events found.")
		return nil
	}
	var ids []string
	for _, item := range events.Items {
		destEvtId, err := cli.create(item)
		if err != nil {
			log.Fatalf("Skipped %s %s: %s", item.Id, item.Summary, err)
		} else if destEvtId == nil {
			log.Printf("Not target: %s", item.Summary)
		} else {
			ids = append(ids, *destEvtId)
		}
	}
	log.Printf("created: %s", strings.Join(ids, ", "))
	return nil
}

func (cli *Client) readToken() (string, error) {
	doc, err := cli.fsCli.Collection("calendar").Doc("channel").Get(cli.ctx)
	if err != nil {
		return "", fmt.Errorf("sync token: %s", err)
	}
	token, _ := doc.Data()["nextSyncToken"].(string)
	return token, nil
}

func (cli *Client) saveToken(syncToken string) error {
	if syncToken == "" {
		return errors.New("cannot save empty nextSyncToken")
	}
	_, err := cli.fsCli.Collection("calendar").Doc("channel").Update(
		cli.ctx,
		[]firestore.Update{{Path: "nextSyncToken", Value: syncToken}},
	)
	if err != nil {
		return fmt.Errorf("sync token: %s", err)
	}
	return nil
}

func (cli *Client) create(srcEvt *calendar.Event) (*string, error) {
	evt := cli.newEvent(srcEvt)
	if evt == nil {
		return nil, nil
	}

	events, err := cli.svc.Events.List(cli.conf.DestCalId).TimeMin(evt.Start.DateTime).TimeMax(evt.End.DateTime).Do()
	if err != nil {
		return nil, fmt.Errorf("list existing events: %w", err)
	}
	existingEvents := events.Items
	isCovered := false
	start, _ := time.Parse(gcalTimeFormat, evt.Start.DateTime)
	end, _ := time.Parse(gcalTimeFormat, evt.End.DateTime)
	for _, existingEvent := range existingEvents {
		startExisting, _ := time.Parse(gcalTimeFormat, existingEvent.Start.DateTime)
		endExisting, _ := time.Parse(gcalTimeFormat, existingEvent.End.DateTime)
		// evtが既存の予定の時間帯に収まるなら作らない
		if !start.Before(startExisting) && !end.After(endExisting) {
			isCovered = true
			break
		}
	}
	if isCovered {
		return nil, nil
	}

	destEvt, err := cli.svc.Events.Insert(cli.conf.DestCalId, evt).Do()
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	return &destEvt.Id, nil
}

const defaultOffset = 30

func (cli *Client) newEvent(srcEvt *calendar.Event) *calendar.Event {
	if srcEvt.Status != "confirmed" { // キャンセル等
		// TODO: キャンセルや変更の場合は作成済みイベントを削除したい
		return nil
	}
	if srcEvt.Start.DateTime == "" { // 終日
		return nil
	}

	start, _ := time.Parse(gcalTimeFormat, srcEvt.Start.DateTime)
	end, _ := time.Parse(gcalTimeFormat, srcEvt.End.DateTime)
	// 週末開始か終了なら無視
	if w := start.Weekday(); w == time.Saturday || w == time.Sunday {
		return nil
	} else if w := end.Weekday(); w == time.Saturday || w == time.Sunday {
		return nil
	} else if (start.Hour() >= 22) || (end.Hour() <= 9) {
		return nil
	}

	matched := false
	for _, rule := range cli.conf.Rules {
		if regexp.MustCompile(rule.Match).MatchString(srcEvt.Summary) {
			if rule.Ignore {
				return nil
			}
			start = add(start, rule.StartOffset)
			end = add(end, rule.EndOffset)
			matched = true
			break
		}
	}
	if !matched {
		// Google Meetなどがない場合はデフォルト
		if srcEvt.ConferenceData.ConferenceSolution == nil {
			start = add(start, -defaultOffset)
			end = add(end, defaultOffset)
		}
	}
	return &calendar.Event{
		Summary: "ブロック",
		Start: &calendar.EventDateTime{
			DateTime: start.Format(gcalTimeFormat),
		},
		End: &calendar.EventDateTime{
			DateTime: end.Format(gcalTimeFormat),
		},
	}
}

func (cli *Client) StartWatch() (string, error) {
	ch, err := cli.newChannel()
	if err != nil {
		return "", err
	}

	res, err := cli.svc.Events.Watch(cli.conf.SrcCalId, ch).Do()
	if err != nil {
		return "", err
	}
	_, err = cli.fsCli.Collection("calendar").Doc("channel").Set(cli.ctx, map[string]interface{}{
		"channelId":  ch.Id,
		"resourceId": res.ResourceId,
		"exp":        res.Expiration,
	})
	if err != nil {
		return "", err
	}
	return res.ResourceId, nil
}

func (cli *Client) newChannel() (*calendar.Channel, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("get random uuid: %s", err)
	}
	// set expiration but forced to about 1 month later
	exp, _ := time.Parse(gcalTimeFormat, "2030-01-01T00:00:00+09:00")
	ch := calendar.Channel{
		Id:         id.String(),
		Type:       "webhook",
		Expiration: exp.UnixNano() / int64(time.Millisecond),
		Address:    cli.conf.Url,
	}
	return &ch, nil
}

func (cli *Client) StopWatch(channelId string, resourceId string) (string, error) {
	ch := calendar.Channel{
		ResourceId: resourceId,
		Id:         channelId,
	}
	err := cli.svc.Channels.Stop(&ch).Do()
	if err != nil {
		return "", err
	}
	log.Printf("stopped channel %s", channelId)
	return channelId, nil
}

func (cli *Client) RenewWatch() (string, error) {
	ch, err := cli.newChannel()
	if err != nil {
		return "", err
	}
	res, err := cli.svc.Events.Watch(cli.conf.SrcCalId, ch).Do()
	if err != nil {
		return "", err
	}

	// get stopping channel
	snap, err := cli.fsCli.Collection("calendar").Doc("channel").Get(cli.ctx)
	if err != nil {
		return "", err
	}
	m := snap.Data()
	log.Println(m["channelId"].(string), m["resourceId"].(string))

	// set new channel
	_, err = cli.fsCli.Collection("calendar").Doc("channel").Set(cli.ctx, map[string]interface{}{
		"channelId":  ch.Id,
		"resourceId": res.ResourceId,
		"exp":        res.Expiration,
	})
	if err != nil {
		return "", err
	}

	// stop channel
	stoppingCh := calendar.Channel{
		ResourceId: m["resourceId"].(string),
		Id:         m["channelId"].(string),
	}
	if err := cli.svc.Channels.Stop(&stoppingCh).Do(); err != nil {
		return "", err
	}
	return "", nil
}

func add(t time.Time, offset int) time.Time {
	return t.Add(time.Duration(offset) * time.Minute)
}
