FROM golang:1.19 AS builder

WORKDIR /app
COPY go.mod go.sum /app/
RUN go mod download
COPY ./pkg ./pkg
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY ./generated ./generated
COPY ./kannon.go  ./

ENV CGO_ENABLED=0
RUN go build -o /build/kannon kannon.go

FROM scratch as kannon
COPY --from=builder  /build/kannon /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]

FROM amacneil/dbmate as migrator-db
COPY ./db /db

FROM amacneil/dbmate as migrator-stats
COPY ./stats_db /db