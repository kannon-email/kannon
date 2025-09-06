package e2e_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-faker/faker/v4"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	statsapiv1 "github.com/kannon-email/kannon/proto/kannon/stats/apiv1"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type clientTest struct {
	mailerClient mailerv1connect.MailerClient
	adminClient  adminv1connect.ApiClient
	hzClient     adminv1connect.HZServiceClient
	statsClient  statsv1connect.StatsApiV1Client
	authToken    string
	domain       string
}

func (c *clientTest) SendEmail(t *testing.T, email *mailerapiv1.SendHTMLReq) {
	sendReq := connect.NewRequest(email)
	sendReq.Header().Set("Authorization", "Basic "+c.authToken)

	sendResp, err := c.mailerClient.SendHTML(t.Context(), sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)

	t.Logf("✅ Email queued with message ID: %s", sendResp.Msg.MessageId)
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
	mailerClient mailerv1connect.MailerClient
	adminClient  adminv1connect.ApiClient
	statsClient  statsv1connect.StatsApiV1Client
	hzClient     adminv1connect.HZServiceClient
}

func (f *clientFactory) NewClient(t *testing.T, infra *TestInfrastructure) *clientTest {
	res, err := f.adminClient.CreateDomain(t.Context(), connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: faker.DomainName(),
	}))
	require.NoError(t, err)

	msg := res.Msg

	return &clientTest{
		mailerClient: f.mailerClient,
		adminClient:  f.adminClient,
		statsClient:  f.statsClient,
		hzClient:     f.hzClient,
		domain:       msg.Domain,
		authToken:    base64.StdEncoding.EncodeToString([]byte(msg.Domain + ":" + msg.Key)),
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

	hzClient := adminv1connect.NewHZServiceClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	return &clientFactory{
		mailerClient: mailerClient,
		adminClient:  adminClient,
		statsClient:  statsClient,
		hzClient:     hzClient,
	}
}
