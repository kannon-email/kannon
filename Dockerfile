FROM golang:1.18 AS builder

WORKDIR /app
COPY go.mod go.sum /app/
RUN go mod download
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY ./generated ./generated

ENV CGO_ENABLED=0
RUN go build -o /build/api cmd/api/*.go
RUN go build -o /build/sender cmd/sender/*.go
RUN go build -o /build/dispatcher cmd/dispatcher/*.go

FROM scratch as api
COPY --from=builder  /build/api /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]

FROM scratch as sender
COPY --from=builder  /build/sender /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]

FROM scratch as dispatcher
COPY --from=builder  /build/dispatcher /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]