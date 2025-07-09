GOBIN=$(PWD)/.bin

.PHONY: test generate-db generate-proto 

test:
	go test ./... -v -short

test-e2e:
	go test ./e2e -v -timeout 10m


generate-db:
	PATH=$(GOBIN) sqlc generate

generate-proto:
	PATH=$(GOBIN) buf generate

generate: generate-db generate-proto

lint:
	golangci-lint run --fix