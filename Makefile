GOBIN=$(PWD)/.bin

.PHONY: test generate-db generate-proto dbstats

test:
	go test ./...

download:
	echo Download go.mod dependencies
	go mod download

install-tools: download
	echo Installing tools from tools.go
	cat tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % env GOBIN=$(GOBIN) go install %

generate-db:
	PATH=$(GOBIN) sqlc generate

generate-proto:
	PATH=$(GOBIN) buf generate

generate: generate-db generate-proto

lint:
	golangci-lint run --fix