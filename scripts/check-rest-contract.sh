#!/bin/sh
set -eu
(cd pkg/api/rest && go generate)
(cd e2e/restclient && go generate)
git diff --exit-code -- pkg/api/rest/types.gen.go e2e/restclient/client.gen.go
# Domain/application packages must not depend on HTTP wire types or adapters.
if go list -f '{{.ImportPath}} {{join .Imports " "}}' ./internal/... | grep -E ' (net/http|connectrpc.com/connect|github.com/kannon-email/kannon/(proto|pkg/api/rest))' | grep -vE '/internal/([^ /]*pb|authzconnect|tests)( |/)' ; then
  echo 'application package imports a transport' >&2
  exit 1
fi
