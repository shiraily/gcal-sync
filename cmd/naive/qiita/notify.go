package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"google.golang.org/api/calendar/v3"
)

func main() {
	http.HandleFunc("/notify", OnNotify)
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

var (
	SrcCalId  = "hoge@example.com" // 同期元
	DestCalId = "fuga@example.com" // 同期先
)

func OnNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	log.Printf("channelId=%s, resourceId=%s", r.Header["X-Goog-Channel-Id"], r.Header["X-Goog-Resource-Id"])

	ctx := context.Background()
	svc := NewCalendarService(ctx)

	resourceState := r.Header["X-Goog-Resource-State"]
	if len(resourceState) > 0 && resourceState[0] == "sync" {
		// 初回syncToken取得
		currentEvents, _ := svc.Events.List(SrcCalId).ShowDeleted(false).
			SingleEvents(true).TimeMin(time.Now().Format(time.RFC3339)).Do()
		fmt.Printf("syncToken: %s\n", currentEvents.NextSyncToken) // 保存する
		return
	}

	syncToken := "" // 保存したtokenを使う
	events, _ := svc.Events.List(SrcCalId).SyncToken(syncToken).Do()
	fmt.Println(syncToken, events.NextSyncToken) // NextSyncTokenは保存しておく

	for _, srcEvt := range events.Items {
		destEvt := newEvent(srcEvt)
		if destEvt == nil {
			continue
		}
		createdEvt, _ := svc.Events.Insert(DestCalId, destEvt).Do()
		fmt.Printf("created %s", createdEvt.Id)
	}

	w.WriteHeader(http.StatusOK)
}

func newEvent(srcEvt *calendar.Event) *calendar.Event {
	if srcEvt.Status != "confirmed" { // キャンセル等を除外
		return nil
	}
	if srcEvt.Start.DateTime == "" { // 終日イベントを除外
		return nil
	}

	// 休日を除外
	start, _ := time.Parse(gcalTimeFormat, srcEvt.Start.DateTime)
	end, _ := time.Parse(gcalTimeFormat, srcEvt.End.DateTime)
	if start.Weekday() == time.Saturday ||
		start.Weekday() == time.Sunday ||
		end.Weekday() == time.Saturday ||
		end.Weekday() == time.Sunday {
		return nil
	}

	// 場所が入っていれば前後30分追加
	if srcEvt.Location != "" {
		start = start.Add(time.Duration(-30) * (time.Minute))
		end = end.Add(time.Duration(30) * (time.Minute))
	}
	startDateTime := start.Format(gcalTimeFormat)
	endDateTime := end.Format(gcalTimeFormat)
	return &calendar.Event{
		Summary: "ブロック",
		Start: &calendar.EventDateTime{
			DateTime: startDateTime,
		},
		End: &calendar.EventDateTime{
			DateTime: endDateTime,
		},
	}
}
