// Package restclient is a client of the published REST contract, generated for the e2e suite only so
// that a handler drifting from the contract fails the suite (ADR 0016).
package restclient

//go:generate go run ../../pkg/api/rest/generate -spec ../../api/openapi/v1.yaml -generate types,client -package restclient -o client.gen.go
