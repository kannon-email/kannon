FROM golang:1.25 AS builder

WORKDIR /app
COPY go.mod go.sum /app/
COPY ./pkg ./pkg
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY ./db ./db
COPY ./proto ./proto
COPY ./kannon.go  ./

ENV CGO_ENABLED=0
RUN go build -o /build/kannon kannon.go

FROM scratch as kannon
COPY --from=builder  /build/kannon /bin/cmd
USER 1000
ENTRYPOINT ["/bin/cmd"]