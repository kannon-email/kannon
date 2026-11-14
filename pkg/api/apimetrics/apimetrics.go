// Package apimetrics counts and times the calls to the Connect and REST APIs. Its labels are bounded
// by construction: the operation is a procedure or a route pattern, never a path, and no label names
// a Domain (ADR 0018).
package apimetrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/httproute"
	"github.com/kannon-email/kannon/x/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const unmatched = "unmatched"

// Metrics holds the API call collectors of one registry.
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New registers the API call collectors on r, or reuses the ones already there.
func New(r prometheus.Registerer) *Metrics {
	return &Metrics{
		requests: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_api_requests_total",
			Help: "Calls to the Connect and REST APIs, by operation and outcome.",
		}, []string{"api", "operation", "code"})),
		duration: metrics.Register(r, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kannon_api_request_duration_seconds",
			Help:    "Time taken to answer a call to the Connect or REST API.",
			Buckets: prometheus.DefBuckets,
		}, []string{"api", "operation"})),
	}
}

func (m *Metrics) observe(api, operation, code string, start time.Time) {
	m.requests.WithLabelValues(api, operation, code).Inc()
	m.duration.WithLabelValues(api, operation).Observe(time.Since(start).Seconds())
}

// Connect is the interceptor that counts every Connect call by procedure and Connect code.
func (m *Metrics) Connect() connect.Interceptor {
	return &interceptor{m: m}
}

type interceptor struct {
	m *Metrics
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		start := time.Now()
		res, err := next(ctx, req)
		i.m.observe("connect", req.Spec().Procedure, connectCode(err), start)
		return res, err
	}
}

func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()
		err := next(ctx, conn)
		i.m.observe("connect", conn.Spec().Procedure, connectCode(err), start)
		return err
	}
}

func connectCode(err error) string {
	if err == nil {
		return "ok"
	}
	return connect.CodeOf(err).String()
}

// REST wraps the REST handler, counting every request by the route pattern it matched and the class
// of its status.
func (m *Metrics) REST(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, route := httproute.Track(r.Context())
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))
		operation := route.Pattern()
		if operation == "" {
			operation = unmatched
		}
		m.observe("rest", operation, strconv.Itoa(rec.status/100)+"xx", start)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.wroteHeader {
		s.status = status
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
