docker-build:
	docker build -t ludusrusso/smtp-api --target api  .
	docker build -t ludusrusso/smtp-sender --target sender  .
	docker build -t ludusrusso/smtp-mailer --target mailer  .

docker-push:
	docker push ludusrusso/smtp-api
	docker push ludusrusso/smtp-sender
	docker push ludusrusso/smtp-mailer