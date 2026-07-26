package tracker

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
)

// The tracker is driven through its real HTTP handlers against the real statssec
// service, because the property under test is what a *signed* token authorises: a
// stub deciding for itself which tokens are acceptable could not tell a forged
// token from a legitimate one.
var (
	testDB *pgxpool.Pool
	ss     statssec.StatsService
)

const (
	testMessageID  = "msg-1@test.com"
	testDomain     = "test.com"
	testRecipient  = "rcpt@example.com"
	testLandingURL = "https://example.com/landing"
	testUserAgent  = "kannon-test-agent/1.0"
	testIP         = "203.0.113.7"
)

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	testDB, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("could not start test postgres", "err", err)
		os.Exit(1)
	}

	ss = statssec.NewStatsService(sqlc.New(testDB))

	code := m.Run()

	if err := purge(); err != nil {
		slog.Error("could not purge test postgres", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// fakePublisher captures what the tracker publishes, decoded the way the Stats
// worker decodes it.
type fakePublisher struct {
	mu    sync.Mutex
	stats []*statstypes.Stats
}

func (p *fakePublisher) Publish(_ string, data []byte) error {
	stat := &statstypes.Stats{}
	if err := proto.Unmarshal(data, stat); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats = append(p.stats, stat)
	return nil
}

func (p *fakePublisher) published() []*statstypes.Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*statstypes.Stats(nil), p.stats...)
}

// engage sends one tracking request through the tracker's routing table, with the
// headers a real recipient's client would carry, and returns the response
// together with everything the tracker published for it.
func engage(t *testing.T, path string) (*http.Response, []*statstypes.Stats) {
	t.Helper()

	pub := &fakePublisher{}
	srv := newServer(pub, ss, Config{})

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("X-Real-Ip", testIP)

	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	return rec.Result(), pub.published()
}

// TestOpenRetainsRequestDataOnlyUnderFull is the compliance property for opens:
// Identified attributes the event to the Recipient and keeps nothing about the
// request, Full additionally keeps the IP address and user agent.
func TestOpenRetainsRequestDataOnlyUnderFull(t *testing.T) {
	for _, tc := range retentionCases() {
		t.Run(tc.name, func(t *testing.T) {
			token, err := ss.CreateOpenToken(t.Context(), testMessageID, testRecipient, tc.mode)
			require.NoError(t, err)

			resp, published := engage(t, "/o/"+token)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			require.Len(t, published, 1)
			stat := published[0]
			assert.Equal(t, testRecipient, stat.Email, "an Identified open is still attributed")
			assert.Equal(t, testMessageID, stat.MessageId)
			assert.Equal(t, testDomain, stat.Domain)
			assert.Equal(t, tc.wantWireMode, stat.TrackingMode, "the event must carry the resolved Mode")

			opened := stat.Data.GetOpened()
			require.NotNil(t, opened)
			assert.Equal(t, tc.wantIP, opened.Ip)
			assert.Equal(t, tc.wantUserAgent, opened.UserAgent)
		})
	}
}

// TestClickRetainsRequestDataOnlyUnderFull is the same property for links, which
// are governed by their own axis of the Policy.
func TestClickRetainsRequestDataOnlyUnderFull(t *testing.T) {
	for _, tc := range retentionCases() {
		t.Run(tc.name, func(t *testing.T) {
			token, err := ss.CreateLinkToken(t.Context(), testMessageID, testRecipient, testLandingURL, tc.mode)
			require.NoError(t, err)

			resp, published := engage(t, "/c/"+token)
			assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
			assert.Equal(t, testLandingURL, resp.Header.Get("Location"))

			require.Len(t, published, 1)
			stat := published[0]
			assert.Equal(t, testRecipient, stat.Email, "an Identified click is still attributed")
			assert.Equal(t, tc.wantWireMode, stat.TrackingMode, "the event must carry the resolved Mode")

			clicked := stat.Data.GetClicked()
			require.NotNil(t, clicked)
			assert.Equal(t, testLandingURL, clicked.Url)
			assert.Equal(t, tc.wantIP, clicked.Ip)
			assert.Equal(t, tc.wantUserAgent, clicked.UserAgent)
		})
	}
}

type retentionCase struct {
	name          string
	mode          tracking.Mode
	wantWireMode  trackingtypes.TrackingMode
	wantIP        string
	wantUserAgent string
}

// retentionCases are the two Modes this ticket separates. The empty wantIP /
// wantUserAgent of the Identified case is the whole point: the event carries the
// Recipient identity and neither field.
func retentionCases() []retentionCase {
	return []retentionCase{
		{
			name:         "Identified",
			mode:         tracking.ModeIdentified,
			wantWireMode: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		},
		{
			name:          "Full",
			mode:          tracking.ModeFull,
			wantWireMode:  trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			wantIP:        testIP,
			wantUserAgent: testUserAgent,
		},
	}
}

// TestForgedModeClaimIsRefused is why the Mode is signed rather than carried in
// the clear: a recipient escalating their own Mode to Full — by rewriting the
// claim, or by re-signing the whole token with a key of their own — gets a refused
// request and no stat at all, rather than a stat with their IP address in it.
func TestForgedModeClaimIsRefused(t *testing.T) {
	openToken, err := ss.CreateOpenToken(t.Context(), testMessageID, testRecipient, tracking.ModeIdentified)
	require.NoError(t, err)
	linkToken, err := ss.CreateLinkToken(t.Context(), testMessageID, testRecipient, testLandingURL, tracking.ModeIdentified)
	require.NoError(t, err)

	foreignKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cases := []struct {
		name string
		path string
	}{
		{name: "OpenWithRewrittenClaim", path: "/o/" + rewriteModeClaim(t, openToken, tracking.ModeFull)},
		{name: "ClickWithRewrittenClaim", path: "/c/" + rewriteModeClaim(t, linkToken, tracking.ModeFull)},
		{name: "OpenResignedWithForeignKey", path: "/o/" + resign(t, openToken, tracking.ModeFull, foreignKey)},
		{name: "ClickResignedWithForeignKey", path: "/c/" + resign(t, linkToken, tracking.ModeFull, foreignKey)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, published := engage(t, tc.path)
			assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a forged token must be refused")
			assert.Empty(t, published, "a refused request must publish no stat")
		})
	}
}

// rewriteModeClaim rewrites the Mode in an already-signed token's payload and
// leaves its header and signature untouched — the cheapest attack available to a
// recipient holding their own token.
func rewriteModeClaim(t *testing.T, token string, mode tracking.Mode) string {
	t.Helper()
	header, claims, signature := splitToken(t, token)
	claims["mode"] = string(mode)
	return strings.Join([]string{header, encodeClaims(t, claims), signature}, ".")
}

// resign re-signs the claims with a key the server does not know, keeping the
// original kid so the tracker does find a public key for the token and then finds
// the signature does not match it.
func resign(t *testing.T, token string, mode tracking.Mode, key *rsa.PrivateKey) string {
	t.Helper()
	header, claims, _ := splitToken(t, token)
	claims["mode"] = string(mode)

	forged := jwt.NewWithClaims(jwt.SigningMethodRS512, jwt.MapClaims(claims))
	forged.Header["kid"] = kidOf(t, header)

	signed, err := forged.SignedString(key)
	require.NoError(t, err)
	return signed
}

func kidOf(t *testing.T, header string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(header)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	kid, ok := decoded["kid"].(string)
	require.True(t, ok, "a minted token must carry a kid")
	return kid
}

func splitToken(t *testing.T, token string) (header string, claims map[string]any, signature string) {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &claims))

	return parts[0], claims, parts[2]
}

func encodeClaims(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// TestRetainedIsGatedOnFull states in one place the rule both handlers share:
// only Full reaches the request itself.
func TestRetainedIsGatedOnFull(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/o/tok", nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("X-Real-Ip", testIP)

	for _, mode := range []tracking.Mode{
		tracking.ModeUnspecified,
		tracking.ModeOff,
		tracking.ModeAnonymous,
		tracking.ModeIdentified,
	} {
		ip, ua := retained(req, mode)
		assert.Empty(t, ip, "Mode %q must retain no IP address", mode)
		assert.Empty(t, ua, "Mode %q must retain no user agent", mode)
	}

	ip, ua := retained(req, tracking.ModeFull)
	assert.Equal(t, testIP, ip)
	assert.Equal(t, testUserAgent, ua)
}
