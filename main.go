package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/google/uuid"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
	"gopkg.in/yaml.v2"
)

func main() {
	http.HandleFunc("/notify", OnNotify)
	http.HandleFunc("/watch", OnWatch)
	http.HandleFunc("/stop", OnStop)
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

const gcalTimeFormat = "2006-01-02T15:04:05-07:00"

type conf struct {
	Url          string `yaml:"url"` // webhook url
	ClientSecret string `yaml:"client_secret"`
	Project      string `yaml:"project"`
	SrcId        string `yaml:"cal_id_private"`
	DestId       string `yaml:"cal_id_business"`
	Rules        []rule `yaml:"rules"`
}

type rule struct {
	Match       string `yaml:"match"`                  // "クリニック"
	StartOffset int    `yaml:"start_offset,omitempty"` // "30" means minute
	EndOffset   int    `yaml:"end_offset,omitempty"`
}

func getConf() *conf {
	var c conf

	yamlFile, err := ioutil.ReadFile("env.yaml")
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return &c
}

func NewCalendarService(ctx context.Context, jsonKey []byte) (*calendar.Service, error) {
	config, err := google.JWTConfigFromJSON(jsonKey, calendar.CalendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("parse client secret file to config: %s", err)
	}
	client := config.Client(ctx)
	return calendar.NewService(ctx, option.WithHTTPClient(client))
}

func (cli *Client) NewFirestoreApp(jsonKey []byte) (*firestore.Client, error) {
	app, err := firebase.NewApp(cli.ctx, &firebase.Config{ProjectID: cli.conf.Project}, option.WithCredentialsJSON(jsonKey))
	if err != nil {
		return nil, err
	}
	return app.Firestore(cli.ctx)
}

func OnNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := NewClient()
	defer cli.fsCli.Close()
	calId, err := cli.Do(len(r.Header["X-Goog-Resource-State"]) > 0 && r.Header["X-Goog-Resource-State"][0] == "exists")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(calId)); err != nil {
		log.Fatal(err)
		return
	}
}

type Client struct {
	ctx    context.Context
	conf   *conf
	calSrv *calendar.Service
	fsCli  *firestore.Client
}

func NewClient() Client {
	cli := &Client{}
	// envs
	cli.conf = getConf()

	cli.ctx = context.Background()
	srv, err := NewCalendarService(cli.ctx, []byte(cli.conf.ClientSecret))
	if err != nil {
		log.Fatalf("Retrieve Calendar client: %s", err)
	}
	cli.calSrv = srv

	cli.fsCli, err = cli.NewFirestoreApp([]byte(cli.conf.ClientSecret))
	if err != nil {
		log.Fatalf("Create firestore cli: %s", err)
	}

	return *cli
}

func (cli *Client) Do(existsSyncToken bool) (string, error) {
	call := cli.calSrv.Events.List(cli.conf.SrcId)
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
		events, err := cli.calSrv.Events.List(cli.conf.SrcId).ShowDeleted(false).
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
		destEvtId, err := cli.SyncEvent(item)
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

func (cli *Client) SyncEvent(srcEvt *calendar.Event) (*string, error) {
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

	destEvt, err := cli.calSrv.Events.Insert(cli.conf.DestId, &evt).Do()
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

func OnWatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := NewClient()
	defer cli.fsCli.Close()
	calId, err := cli.StartWatch()
	if err != nil {
		log.Fatalf("Start watch: %s", err)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(calId)); err != nil {
		log.Fatal(err)
		return
	}
}

func (cli *Client) StartWatch() (string, error) {
	ch, err := cli.newChannel()
	if err != nil {
		return "", err
	}

	res, err := cli.calSrv.Events.Watch(cli.conf.SrcId, ch).Do()
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

func OnStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := NewClient()
	defer cli.fsCli.Close()
	channelId, err := cli.StopWatch(r.FormValue("channel-id"), r.FormValue("resource-id"))
	if err != nil {
		log.Fatalf("Start watch: %s", err)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(channelId)); err != nil {
		log.Fatal(err)
	}
}

func (cli *Client) StopWatch(channelId string, resourceId string) (string, error) {
	ch := calendar.Channel{
		ResourceId: resourceId,
		Id:         channelId,
	}
	err := cli.calSrv.Channels.Stop(&ch).Do()
	if err != nil {
		return "", err
	}
	log.Printf("stopped channel %s", channelId)
	return channelId, nil
}

func OnRenew(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := NewClient()
	defer cli.fsCli.Close()
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

func (cli *Client) RenewWatch() (string, error) {
	ch, err := cli.newChannel()
	if err != nil {
		return "", err
	}
	res, err := cli.calSrv.Events.Watch(cli.conf.SrcId, ch).Do()
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
	if err := cli.calSrv.Channels.Stop(&stoppingCh).Do(); err != nil {
		return "", err
	}
	return "", nil
}
