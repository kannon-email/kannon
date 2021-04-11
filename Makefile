.PHONY: test db proto

proto: proto/*.proto
	buf generate

test:
	go test ./...

db:
	sqlc generate

docker-build:
	docker build -t ghcr.io/gyozatech/kannon/api --target api  .
	docker build -t ghcr.io/gyozatech/kannon/sender --target sender  .
	docker build -t ghcr.io/gyozatech/kannon/mailer --target mailer  .

docker-push:
	docker push ghcr.io/gyozatech/kannon/api
	docker push ghcr.io/gyozatech/kannon/sender
	docker push ghcr.io/gyozatech/kannon/mailer
