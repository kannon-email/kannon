FROM golang:1.27.0 AS builder

WORKDIR /app

# Copy dependency files first for better caching
COPY go.mod go.sum /app/
RUN go mod download

# Then copy source files
COPY ./pkg ./pkg
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY ./x ./x
COPY ./db ./db
COPY ./proto ./proto
COPY ./kannon.go  ./

ENV CGO_ENABLED=0
RUN go build -o /build/kannon kannon.go

FROM scratch AS kannon
# scratch ships no trust store: without it every certificate check fails, so
# outbound STARTTLS always fell back to an unverified session and Postgres or
# NATS could not be reached with a verified TLS connection.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder  /build/kannon /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]