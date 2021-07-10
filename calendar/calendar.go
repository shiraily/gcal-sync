package calendar

import (
	"context"
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
	gcalTimeFormat         = "2006-01-02T15:04:05-07:00"
	calendarScopeWithOAuth = "https://www.googleapis.com/auth/calendar.events.owned"
	calendarId             = "primary"
	oauthClientSecret      = "credentials.json"
)

var (
	serviceAccountClientSecret = "service_account_credentials.json"
)

type Client struct {
	ctx        context.Context
	conf       *config.Config
	srcCalSrv  *calendar.Service
	destCalSrv *calendar.Service
	fsCli      *firestore.Client
}

func NewClient() Client {
	cli := &Client{}
	// envs
	cli.conf = config.GetConfig()
	cli.ctx = context.Background()

	srcSrv, err := NewCalendarService(cli.ctx, oauthClientSecret, cli.conf.SrcTokenFile)
	if err != nil {
		log.Fatalf("Retrieve Calendar client: %s", err)
	}
	cli.srcCalSrv = srcSrv

	destSrv, err := NewCalendarService(cli.ctx, oauthClientSecret, cli.conf.DestTokenFile)
	if err != nil {
		log.Fatalf("Retrieve Calendar client: %s", err)
	}
	cli.destCalSrv = destSrv

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

// 参考: サービスアカウントのみを用いる場合。APIコール時にはアクセス対象のカレンダーIDが必要 (要実装)
func NewCalendarServiceWithServiceAccount(ctx context.Context, credentialFile string) (*calendar.Service, error) {
	b, err := ioutil.ReadFile(credentialFile)
	if err != nil {
		return nil, fmt.Errorf("read client secret file: %s", err)
	}
	config, err := google.JWTConfigFromJSON(b, calendar.CalendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("parse client secret file to config: %s", err)
	}
	client := config.Client(ctx)
	return calendar.NewService(ctx, option.WithHTTPClient(client))
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

func (cli *Client) Sync(existsSyncToken bool) (string, error) {
	call := cli.srcCalSrv.Events.List(calendarId)
	if existsSyncToken {
		// get token from firestore
		doc, err := cli.fsCli.Collection("calendar").Doc("channel").Get(cli.ctx)
		if err != nil {
			return "", fmt.Errorf("sync token: %s", err)
		}
		nextToken, _ := doc.Data()["nextSyncToken"].(string)
		call = call.SyncToken(nextToken)
	} else {
		t := time.Now().Format(time.RFC3339)
		events, err := cli.srcCalSrv.Events.List(calendarId).ShowDeleted(false).
			SingleEvents(true).TimeMin(t).Do()
		if err != nil {
			return "", fmt.Errorf("get first token: %s", err)
		}
		call = call.SyncToken(events.NextSyncToken)
	}

	events, err := call.Do()
	if err != nil {
		return "", fmt.Errorf("retrieve next events: %s", err)
	}

	// set token
	_, err = cli.fsCli.Collection("calendar").Doc("channel").Update(
		cli.ctx,
		[]firestore.Update{{Path: "nextSyncToken", Value: events.NextSyncToken}},
	)
	if err != nil {
		return "", fmt.Errorf("sync token: %s", err)
	}

	if len(events.Items) == 0 {
		fmt.Println("No upcoming events found.")
		return "", nil
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
	return strings.Join(ids, ", "), nil
}

func (cli *Client) create(srcEvt *calendar.Event) (*string, error) {
	start, end := cli.getEventTime(srcEvt)
	if start == "" {
		return nil, nil
	}

	evt := calendar.Event{
		Summary: "ブロック",
		Start: &calendar.EventDateTime{
			DateTime: start,
		},
		End: &calendar.EventDateTime{
			DateTime: end,
		},
	}

	destEvt, err := cli.destCalSrv.Events.Insert(calendarId, &evt).Do()
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	return &destEvt.Id, nil
}

func (cli *Client) getEventTime(srcEvt *calendar.Event) (string, string) {
	if srcEvt.Status != "confirmed" { // キャンセル等
		// TODO: キャンセルや変更の場合は作成済みイベントを削除したい
		return "", ""
	}
	if srcEvt.Start.DateTime == "" { // 終日
		return "", ""
	}

	start, _ := time.Parse(gcalTimeFormat, srcEvt.Start.DateTime)
	end, _ := time.Parse(gcalTimeFormat, srcEvt.End.DateTime)
	if start.Weekday() == time.Saturday ||
		start.Weekday() == time.Sunday ||
		end.Weekday() == time.Saturday ||
		end.Weekday() == time.Sunday {
		return "", ""
	}

	if srcEvt.Location != "" {
		return start.Add(time.Duration(-30) * (time.Minute)).Format(gcalTimeFormat),
			end.Add(time.Duration(30) * (time.Minute)).Format(gcalTimeFormat)
	} else {
		for _, rule := range cli.conf.Rules {
			if regexp.MustCompile(rule.Match).MatchString(srcEvt.Summary) {
				return start.Add(time.Duration(rule.StartOffset) * time.Minute).Format(gcalTimeFormat),
					end.Add(time.Duration(rule.EndOffset) * time.Minute).Format(gcalTimeFormat)
			}
		}
	}
	return srcEvt.Start.DateTime, srcEvt.End.DateTime
}

func (cli *Client) StartWatch() (string, error) {
	ch, err := cli.newChannel()
	if err != nil {
		return "", err
	}

	res, err := cli.srcCalSrv.Events.Watch(calendarId, ch).Do()
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
	err := cli.srcCalSrv.Channels.Stop(&ch).Do()
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
	res, err := cli.srcCalSrv.Events.Watch(calendarId, ch).Do()
	if err != nil {
		return "", err
	}

	// get stopping channel
	snap, err := cli.fsCli.Collection("calendar").Doc("channel").Get(cli.ctx)
	if err != nil {
		return "", err
	}
	m := snap.Data()
	fmt.Println(m["channelId"].(string), m["resourceId"].(string))

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
	if err := cli.srcCalSrv.Channels.Stop(&stoppingCh).Do(); err != nil {
		return "", err
	}
	return "", nil
}
