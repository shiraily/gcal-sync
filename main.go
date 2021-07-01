package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
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
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := config.Client(ctx)
	return calendar.NewService(ctx, option.WithHTTPClient(client))
}

func (cli *Client) NewFirestoreApp(jsonKey []byte) (*firestore.Client, error) {
	/*config, err := google.JWTConfigFromJSON(jsonKey, calendar.CalendarEventsScope)
	if err != nil {
		return nil, err
	}*/
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
	calId := cli.Do()
	if calId == "" {
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
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}
	cli.calSrv = srv

	cli.fsCli, err = cli.NewFirestoreApp([]byte(cli.conf.ClientSecret))
	if err != nil {
		log.Fatalf("Create firestore cli: %s", err)
	}

	return *cli
}

func (cli *Client) Do() string {

	t := time.Now().Format(time.RFC3339)
	events, err := cli.calSrv.Events.List(cli.conf.SrcId).ShowDeleted(false).
		SingleEvents(true).TimeMin(t).MaxResults(10).OrderBy("startTime").Do()
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

	srcEvt := events.Items[0]

	destEvt := cli.CreateEvent(srcEvt)
	return *destEvt
}

func (cli *Client) CreateEvent(srcEvt *calendar.Event) *string {
	evt := calendar.Event{
		Summary: "ブロック",
		Start:   srcEvt.Start,
		End:     srcEvt.End,
	}
	if evt.Start.DateTime == "" { // 終日
		return nil
	}

	start, _ := time.Parse(gcalTimeFormat, evt.Start.DateTime)
	end, _ := time.Parse(gcalTimeFormat, evt.End.DateTime)
	if start.Weekday() == time.Saturday ||
		start.Weekday() == time.Sunday ||
		end.Weekday() == time.Saturday ||
		end.Weekday() == time.Sunday {
		return nil
	}

	for _, rule := range cli.conf.Rules {
		if regexp.MustCompile(rule.Match).MatchString(srcEvt.Summary) {
			evt.Start = &calendar.EventDateTime{
				DateTime: start.Add(time.Duration(rule.StartOffset) * time.Minute).Format(gcalTimeFormat),
			}
			evt.End = &calendar.EventDateTime{
				DateTime: end.Add(time.Duration(rule.EndOffset) * time.Minute).Format(gcalTimeFormat),
			}
			break
		}
	}

	destEvt, err := cli.calSrv.Events.Insert(cli.conf.DestId, &evt).Do()
	if err != nil {
		log.Fatalf("failed to create, %s", err)
	}
	log.Printf("succeeded in creating event from: %s", evt.Id)
	return &destEvt.Id
}

func OnWatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	cli := NewClient()
	defer cli.fsCli.Close()
	calId, err := cli.StartWatch()
	if err != nil {
		log.Fatalf("Start watch: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := w.Write([]byte(calId)); err != nil {
		log.Fatal(err)
		return
	}
}

func (cli *Client) StartWatch() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	// may be about 1 month later
	exp, _ := time.Parse(gcalTimeFormat, "2030-01-01T00:00:00+09:00")
	ch := calendar.Channel{
		Id:         id.String(),
		Type:       "webhook",
		Expiration: exp.UnixNano() / int64(time.Millisecond),
		Address:    cli.conf.Url,
	}
	res, err := cli.calSrv.Events.Watch(cli.conf.SrcId, &ch).Do()
	if err != nil {
		return "", err
	}
	rid := res.ResourceId
	log.Printf("Create Resource: %+v", res)
	result, err := cli.fsCli.Collection("calendar").Doc("channel").Set(cli.ctx, map[string]interface{}{
		"resourceId": res.ResourceId,
		"exp":        res.Expiration,
	})
	if err != nil {
		return "", err
	}
	log.Printf("write to firestore: %+v", result)
	return rid, nil
}
