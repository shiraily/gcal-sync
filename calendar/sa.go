package calendar

import (
	"context"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// サービスアカウントのみを用いる場合。APIコール時にはアクセス対象のカレンダーIDが必要
func NewCalendarServiceWithServiceAccount(ctx context.Context, credentialFile string) (*calendar.Service, error) {
	return calendar.NewService(ctx, option.WithCredentialsFile(credentialFile))
}
