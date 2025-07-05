GOBIN=$(PWD)/.bin

.PHONY: test generate-db generate-proto 

test:
	go test ./...


generate-db:
	PATH=$(GOBIN) sqlc generate

generate-proto:
	PATH=$(GOBIN) buf generate

generate: generate-db generate-proto

lint:
	golangci-lint run --fix