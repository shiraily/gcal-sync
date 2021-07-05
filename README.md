[日本語](https://github.com/shiraily/gcal-sync/blob/main/README.ja.md)

# gcal-sync

gcal-sync synchronizes Google Calendar events to another calendar (also in another account) via Google Calendar API.

You can set simple rules to adjust time of created event.

# Usecase

- Create an event on business calendar to block a period when an event is created on private calendar.

# Motivation

It bothers me to create an event on calendar for business just for blocking the period when I create an event on calendar for private.
You can do this by using like Zapier if you want to sync ALL new events to another account.
But for example, it is difficult & bothered to set complicated rules on Zapier. Plus, executing such task needs paid plan!

# Requirements

- Go
- Google Cloud services
  - App Engine
  - Firestore
  - Cloud Scheduler

# Setup

### Register domain

You must register the domain for Push notification channel URL of Google Calendar.
Set static/googlexxx.html file.

See also [Registering your domain](https://developers.google.com/calendar/api/guides/push#registering-your-domain)

### Enable Google Calendar API

https://console.cloud.google.com/apis/library/calendar-json.googleapis.com

### Create env.yaml file

Refer sample.env.yaml file

# Deploy

### App & Cron

Before deploy an app, set .env file according to .sample.env file.

```
make deploy & Cron
```