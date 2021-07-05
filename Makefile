include .env

deploy:
	gcloud app deploy --version 1 -q

schedule:
	gcloud scheduler jobs create app-engine gcal-sync --schedule="0 5 * * 2" --relative-url="/renew" --service=$(SERVICE) --time-zone="Asia/Tokyo"
