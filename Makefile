.PHONY: test db proto

db:
	sqlc generate

proto: proto/*.proto
	buf generate

test:
	go test ./...
