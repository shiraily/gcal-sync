package main

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/calendar/v3"
)

func Notify() {
	ctx := context.Background()
	svc := NewCalendarService(ctx, "src_token.json")
	t := time.Now().Format(time.RFC3339)
	currentEvents, _ := svc.Events.List(CalendarId).ShowDeleted(false).
		SingleEvents(true).TimeMin(t).Do()

	events, _ := svc.Events.List(CalendarId).SyncToken(currentEvents.NextSyncToken).Do()
	fmt.Println(events.NextSyncToken) // saveしておく

	svc2 := NewCalendarService(ctx, "dest_token.json")

	for _, srcEvt := range events.Items {
		destEvt := newEvent(srcEvt)
		if destEvt == nil {
			continue
		}
		createdEvt, _ := svc2.Events.Insert(CalendarId, destEvt).Do()
		fmt.Printf("created %s", createdEvt.Id)
	}
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
