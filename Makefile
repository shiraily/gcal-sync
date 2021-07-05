include .env

deploy:
	gcloud app deploy --version 1 -q
