//go:build tools
// +build tools

package main

import (
	_ "github.com/buphbuild/buph/cmd/buph"
	_ "github.com/sqlc-dev/sqlc/cmd/sqlc"
	_ "github.com/vektra/mockery/v2"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuph/cmd/protoc-gen-go"
)
