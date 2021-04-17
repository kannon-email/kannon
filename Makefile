.PHONY: test db proto

proto: proto/*.proto
	buf generate

test:
	go test ./...
