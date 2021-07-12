[日本語](https://github.com/shiraily/gcal-sync/blob/main/README.ja.md)

# gcal-sync

gcal-sync synchronizes Google Calendar events to another calendar (also in another account) via Google Calendar API.

You can set simple rules to adjust time of creating event.

# Usecase

- Create an event on business calendar to block the time box when an event is created on private calendar.

# Motivation

It bothers me to create an event on calendar for business just for blocking the time box when I create an event on calendar on private.
You can do this by using like Zapier if you want to sync ALL new events to another account.
But for example, it is difficult & bothered to set complicated rules on Zapier. Plus, executing such task needs paid plan!

# Requirements

- Go
- Google Cloud services
  - App Engine
  - Firestore
  - Cloud Scheduler

# Setup

### Create OAuth client

Needs

- OAuth consent screen
  - scope: `https://www.googleapis.com/auth/calendar.events.owned`
- OAuth client secret

### Register domain

You must register the domain for Push notification channel URL of Google Calendar.
Set static/googlexxx.html file for registration.

See also [Registering your domain](https://developers.google.com/calendar/api/guides/push#registering-your-domain)

### Enable Google Calendar API

https://console.cloud.google.com/apis/library/calendar-json.googleapis.com

### Issue service account key

For accessing to Firestore, needs service account key with Firebase Admin SDK role.

# Deploy

### Create setting files

- env.yaml: sample is sample.env.yaml
- .env: sample is .sample.env
- app.yaml: sample is sample.app.yaml

### Get token

```
go run cmd/token/token.go src # for getting token of source calendar
go run cmd/token/token.go dest # for destination calendar
```

### App Engine & Cloud Scheduler

```
make deploy
make schedule
```

# Use

### Register webhook URL

```
go run cmd/watch/watch.go
```

### Stop webhook channel

For some reason, you may want to stop some channels:

```
go run cmd/stop/stop.go channel-id resource-id
```
