package main

import (
	"context"

	"google.golang.org/api/calendar/v3"
)

func Stop() {
	ctx := context.Background()
	svc := NewCalendarService(ctx)
	svc.Channels.Stop(&calendar.Channel{
		ResourceId: "resource-id",
		Id:         "channel-id",
	})
}
