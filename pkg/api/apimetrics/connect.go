package apimetrics

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ConnectService wraps the handler connect-go generated for one service, mounted at path, so that
// the calls connect-go refuses before any interceptor runs are counted too: an oversized or
// undecodable body, a protocol or content type it does not speak, a procedure the service lacks.
// It takes and returns the pair a generated New...Handler returns, to be handed to ServeMux.Handle.
func (m *Metrics) ConnectService(path string, next http.Handler) (string, http.Handler) {
	procedures := serviceProcedures(path)
	return path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		call := &interceptedCall{}
		rec := &errorRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), interceptedKey{}, call)))
		if call.seen {
			return
		}
		operation := unmatched
		if procedures[r.URL.Path] {
			operation = r.URL.Path
		}
		m.observe("connect", operation, rec.code(), start)
	})
}

// serviceProcedures lists the procedures of the service a generated handler path names, read from
// the service's registered descriptor.
func serviceProcedures(path string) map[string]bool {
	name := protoreflect.FullName(strings.Trim(path, "/"))
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err != nil {
		panic("apimetrics: no registered service for Connect path " + path + ": " + err.Error())
	}
	service, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		panic("apimetrics: Connect path " + path + " does not name a service")
	}
	procedures := map[string]bool{}
	for i := range service.Methods().Len() {
		procedures[path+string(service.Methods().Get(i).Name())] = true
	}
	return procedures
}

type interceptedKey struct{}

// interceptedCall is marked by the interceptor, telling ConnectService the call is already counted.
type interceptedCall struct {
	seen bool
}

func markIntercepted(ctx context.Context) {
	if call, ok := ctx.Value(interceptedKey{}).(*interceptedCall); ok {
		call.seen = true
	}
}

// errorBodyLimit bounds what errorRecorder keeps of an error response: a Connect error is a small
// JSON object, and only its code is read.
const errorBodyLimit = 1 << 10

// errorRecorder keeps what it needs to name the Connect code of a response connect-go wrote without
// the interceptor seeing the call.
type errorRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (e *errorRecorder) WriteHeader(status int) {
	if !e.wroteHeader {
		e.status = status
		e.wroteHeader = true
	}
	e.ResponseWriter.WriteHeader(status)
}

func (e *errorRecorder) Write(b []byte) (int, error) {
	e.wroteHeader = true
	if e.status != http.StatusOK && e.body.Len() < errorBodyLimit {
		e.body.Write(b[:min(len(b), errorBodyLimit-e.body.Len())])
	}
	return e.ResponseWriter.Write(b)
}

func (e *errorRecorder) Flush() {
	_ = http.NewResponseController(e.ResponseWriter).Flush()
}

func (e *errorRecorder) Unwrap() http.ResponseWriter {
	return e.ResponseWriter
}

// code reads the Connect code from where each protocol puts it: the gRPC status header or trailer,
// the Connect error body, and failing both the HTTP status, mapped the way a Connect client maps it.
func (e *errorRecorder) code() string {
	for _, key := range []string{"Grpc-Status", http.TrailerPrefix + "Grpc-Status"} {
		if raw := e.Header().Get(key); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 32)
			if err != nil {
				return connect.CodeUnknown.String()
			}
			if n == 0 {
				return "ok"
			}
			return boundedCode(connect.Code(n))
		}
	}
	var wire struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(e.body.Bytes(), &wire) == nil {
		var c connect.Code
		if c.UnmarshalText([]byte(wire.Code)) == nil {
			return boundedCode(c)
		}
	}
	if e.status == http.StatusOK {
		return "ok"
	}
	return httpCode(e.status).String()
}

// boundedCode keeps the label to the codes Connect defines: connect.Code parses any number.
func boundedCode(c connect.Code) string {
	if c < connect.CodeCanceled || c > connect.CodeUnauthenticated {
		return connect.CodeUnknown.String()
	}
	return c.String()
}

// httpCode maps an HTTP status as a Connect client does, for responses that carry no Connect code.
func httpCode(status int) connect.Code {
	switch status {
	case http.StatusBadRequest:
		return connect.CodeInternal
	case http.StatusUnauthorized:
		return connect.CodeUnauthenticated
	case http.StatusForbidden:
		return connect.CodePermissionDenied
	case http.StatusNotFound:
		return connect.CodeUnimplemented
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return connect.CodeUnavailable
	default:
		return connect.CodeUnknown
	}
}
