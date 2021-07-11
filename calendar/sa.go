package calendar

import (
	"context"
	"fmt"
	"io/ioutil"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

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
