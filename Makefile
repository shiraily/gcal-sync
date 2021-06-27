include .env

deploy:
	go mod vendor
	gcloud functions deploy $(FUNCTION) \
					--entry-point OnWatch \
					--runtime go113 \
					--project $(PROJECT) \
					--region asia-northeast1 \
					--timeout=30s \
					--trigger-http \
					--allow-unauthenticated
