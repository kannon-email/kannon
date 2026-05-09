GOBIN=$(PWD)/.bin

.PHONY: test generate-db generate-proto upgrade lint deadcode

upgrade:
	go get -u ./...
	go mod tidy
	mise upgrade

test:
	go test ./... -race -v -short

test-e2e:
	go test ./e2e -race -v -timeout 10m


generate-db:
	sqlc generate

generate-proto:
	buf generate

generate: generate-db generate-proto

lint: deadcode
	golangci-lint run --fix

deadcode:
	@out=$$(go tool deadcode -test ./...); \
	if [ -n "$$out" ]; then \
		echo "$$out"; \
		echo "deadcode: unreachable code detected"; \
		exit 1; \
	fi

docker-up:
	# KANNON_IMAGE default to ghcr.io/kannon-email/kannon:latest
	docker compose -f examples/docker-compose/docker-compose.yaml up