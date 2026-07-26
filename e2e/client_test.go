package e2e_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/tests"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statsapiv1 "github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	statsapiv2 "github.com/kannon-email/kannon/proto/kannon/stats/apiv2"
	statsv2connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultSenderAlias = "Test Sender"

type clientTest struct {
	mailerClient  mailerv1connect.MailerClient
	adminClient   adminv1connect.ApiClient
	hzClient      adminv1connect.HZServiceClient
	statsClient   statsv1connect.StatsApiV1Client
	statsV2Client statsv2connect.StatsApiV2Client
	authToken     string
	domain        string
}

// Sender returns the default sender for the client's domain. Tests
// compose this into SendHTMLReq.Sender so the local-part stays
// consistent with what SenderFrom asserts on the receiving end.
func (c *clientTest) Sender() *mailertypes.Sender {
	return &mailertypes.Sender{
		Email: "sender@" + c.domain,
		Alias: defaultSenderAlias,
	}
}

// SenderFrom returns the RFC-5322 "Alias <addr>" rendering of Sender(),
// matching what the SMTP layer puts in the From header.
func (c *clientTest) SenderFrom() string {
	return fmt.Sprintf("%s <sender@%s>", defaultSenderAlias, c.domain)
}

// SetTrackingPolicy configures the Tracking Policy of the client's Domain
// through the Admin API, the way an operator would. It must be called before a
// send: the Policy is resolved at intake and frozen on each Delivery, so it does
// not reach Deliveries already in the Pool.
func (c *clientTest) SetTrackingPolicy(t *testing.T, p *trackingtypes.TrackingPolicy) {
	t.Helper()
	_, err := c.adminClient.SetTrackingPolicy(t.Context(), connect.NewRequest(&adminapiv1.SetTrackingPolicyReq{
		Domain:   c.domain,
		Tracking: p,
	}))
	require.NoError(t, err)
}

// SendEmail submits the request and returns the Batch id (message_id) so
// callers can correlate pool/stats state for their own Batch.
func (c *clientTest) SendEmail(t *testing.T, email *mailerapiv1.SendHTMLReq) string {
	sendReq := connect.NewRequest(email)
	sendReq.Header().Set("Authorization", "Basic "+c.authToken)

	sendResp, err := c.mailerClient.SendHTML(t.Context(), sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)

	t.Logf("✅ Email queued with message ID: %s", sendResp.Msg.MessageId)
	return sendResp.Msg.MessageId
}

func (f *clientTest) GetAggregatedStats(t *testing.T) *statsapiv2.GetAggregatedStatsRes {
	// Aggregated stats are bucketed by day (truncated to midnight UTC),
	// so the query range must span at least a full day to include today's bucket.
	resp, err := f.statsV2Client.GetAggregatedStats(t.Context(), connect.NewRequest(&statsapiv2.GetAggregatedStatsReq{
		Domain:   f.domain,
		FromDate: timestamppb.New(time.Now().Add(-24 * time.Hour)),
		ToDate:   timestamppb.New(time.Now().Add(24 * time.Hour)),
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg)

	return resp.Msg
}

func (f *clientTest) GetStats(t *testing.T) *statsapiv1.GetStatsRes {
	td := time.Hour

	statsResp, err := f.statsClient.GetStats(t.Context(), connect.NewRequest(&statsapiv1.GetStatsReq{
		Domain:   f.domain,
		Skip:     0,
		Take:     10000,
		FromDate: timestamppb.New(time.Now().Add(-td)),
		ToDate:   timestamppb.New(time.Now().Add(td)),
	}))
	require.NoError(t, err)
	require.NotNil(t, statsResp.Msg)

	return statsResp.Msg
}

type clientFactory struct {
	mailerClient  mailerv1connect.MailerClient
	adminClient   adminv1connect.ApiClient
	statsClient   statsv1connect.StatsApiV1Client
	statsV2Client statsv2connect.StatsApiV2Client
	hzClient      adminv1connect.HZServiceClient
}

func (f *clientFactory) NewClient(t *testing.T, infra *TestInfrastructure) *clientTest {
	domain := tests.FakeDomain(t)
	res, err := f.adminClient.CreateDomain(t.Context(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: domain,
	}))
	require.NoError(t, err)

	keyRes, err := f.adminClient.CreateAPIKey(t.Context(), connect.NewRequest(&adminapiv1.CreateAPIKeyRequest{
		Domain: domain,
		Name:   "test-key",
	}))
	require.NoError(t, err)

	key := keyRes.Msg.Key

	authToken := base64.StdEncoding.EncodeToString([]byte(domain + ":" + key))

	msg := res.Msg

	return &clientTest{
		mailerClient:  f.mailerClient,
		adminClient:   f.adminClient,
		statsClient:   f.statsClient,
		statsV2Client: f.statsV2Client,
		hzClient:      f.hzClient,
		domain:        msg.Domain,
		authToken:     authToken,
	}
}

func makeFactory(infra *TestInfrastructure) *clientFactory {
	adminClient := adminv1connect.NewApiClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	mailerClient := mailerv1connect.NewMailerClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	statsClient := statsv1connect.NewStatsApiV1Client(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	statsV2Client := statsv2connect.NewStatsApiV2Client(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	hzClient := adminv1connect.NewHZServiceClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	return &clientFactory{
		mailerClient:  mailerClient,
		adminClient:   adminClient,
		statsClient:   statsClient,
		statsV2Client: statsV2Client,
		hzClient:      hzClient,
	}
}
