GOBIN=$(PWD)/.bin

.PHONY: test generate-db generate-proto upgrade

upgrade:
	go get -u ./...
	go mod tidy
	mise upgrade

test:
	go test ./... -v -short

test-e2e:
	go test ./e2e -v -timeout 10m


generate-db:
	sqlc generate

generate-proto:
	buf generate

generate: generate-db generate-proto

lint:
	golangci-lint run --fix