package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authzconnect"
	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// The Mailer API authenticates inside its handler, so whatever connect-go reads before calling it is
// read for anybody. An oversized body is refused there, and the handler never runs.
func TestAnOversizedSendIsRefusedBeforeTheMailerHandler(t *testing.T) {
	mailer, srv := readLimitServer(t)

	body := marshal(t, &mailerv1.SendHTMLReq{Html: strings.Repeat("a", connectReadMaxBytes(defaultMaxPayload))})
	status, code := postConnect(t, srv.URL+mailerv1connect.MailerSendHTMLProcedure, body, false)

	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, connect.CodeResourceExhausted.String(), code)
	assert.Zero(t, mailer.calls.Load())
}

// A gzip bomb is a few kilobytes on the wire, which is all a proxy's body cap ever sees. The limit
// is on what the body inflates to, so it is refused all the same.
func TestAGzipBombIsRefusedBeforeTheMailerHandler(t *testing.T) {
	mailer, srv := readLimitServer(t)

	body := marshal(t, &mailerv1.SendHTMLReq{Html: strings.Repeat("a", connectReadMaxBytes(defaultMaxPayload))})
	compressed := gzipped(t, body)
	require.Less(t, len(compressed), connectReadMaxBytes(defaultMaxPayload)/100)

	status, code := postConnect(t, srv.URL+mailerv1connect.MailerSendHTMLProcedure, compressed, true)

	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, connect.CodeResourceExhausted.String(), code)
	assert.Zero(t, mailer.calls.Load())
}

// The admin token is checked by an interceptor, which runs after the body is decoded: an
// unauthenticated caller is refused for its size, not its credential, because the size is
// checked first.
func TestAnOversizedAdminRequestIsRefusedBeforeAuthentication(t *testing.T) {
	_, srv := readLimitServer(t)

	body := marshal(t, &adminv1.CreateDomainRequest{Domain: strings.Repeat("a", connectReadMaxBytes(defaultMaxPayload))})
	status, code := postConnect(t, srv.URL+adminv1connect.ApiCreateDomainProcedure, gzipped(t, body), true)

	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, connect.CodeResourceExhausted.String(), code)
}

// The limit sits above what intake accepts: a send carrying max_payload of content, split between
// HTML and an attachment, with a long Recipient list besides, still reaches the handler.
func TestASendAtMaxPayloadStillReachesTheMailerHandler(t *testing.T) {
	mailer, srv := readLimitServer(t)

	recipients := make([]*mailertypes.Recipient, 10_000)
	for i := range recipients {
		recipients[i] = &mailertypes.Recipient{Email: "someone@example.com", Fields: map[string]string{"name": "Someone"}}
	}
	half := int(defaultMaxPayload) / 2
	req := &mailerv1.SendHTMLReq{
		Html:        strings.Repeat("a", half),
		Attachments: []*mailerv1.Attachment{{Filename: "a.bin", Content: bytes.Repeat([]byte{1}, half*3/4)}},
		Recipients:  recipients,
	}

	for _, opts := range [][]connect.ClientOption{nil, {connect.WithSendGzip()}} {
		client := mailerv1connect.NewMailerClient(srv.Client(), srv.URL, opts...)
		_, err := client.SendHTML(t.Context(), connect.NewRequest(req))
		require.NoError(t, err)
	}
	assert.EqualValues(t, 2, mailer.calls.Load())
}

// countingMailer stands in for the Mailer API, counting the requests that got as far as it.
type countingMailer struct {
	mailerv1connect.UnimplementedMailerHandler
	calls atomic.Int64
}

func (m *countingMailer) SendHTML(context.Context, *connect.Request[mailerv1.SendHTMLReq]) (*connect.Response[mailerv1.SendRes], error) {
	m.calls.Add(1)
	return connect.NewResponse(&mailerv1.SendRes{}), nil
}

// readLimitServer serves the API's mux with the limit a default configuration gets and a real
// admin token check, so that what is under test is the wiring rather than a copy of it.
func readLimitServer(t *testing.T) (*countingMailer, *httptest.Server) {
	t.Helper()
	mailer := &countingMailer{}
	mux := newAPIMux(
		connectReadMaxBytes(defaultMaxPayload),
		authzconnect.AdminTokenHandlerOptions(admintoken.MustParse("secret")),
		adminv1connect.UnimplementedApiHandler{},
		mailer,
		apiv1connect.UnimplementedStatsApiV1Handler{},
		apiv2connect.UnimplementedStatsApiV2Handler{},
		adminv1connect.UnimplementedHZServiceHandler{},
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return mailer, srv
}

// postConnect sends a unary Connect request by hand and without credentials, so that the body and
// its encoding are exactly what the test says rather than what a client would choose. It returns
// the HTTP status and the Connect error code the server answered with.
func postConnect(t *testing.T, url string, body []byte, gzipEncoded bool) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/proto")
	if gzipEncoded {
		req.Header.Set("Content-Encoding", "gzip")
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	var wire struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&wire))
	return res.StatusCode, wire.Code
}

func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return b
}

func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(b)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}
