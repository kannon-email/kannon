.PHONY: test db proto dbstats

db:
	sqlc generate

proto: proto/*.proto
	buf generate

test:
	go test ./...
