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
	// testPseudonym has the shape ADR 0006 fixes for a Pseudonymous identity: 128
	// random bits as a lowercase hex local part, under the sending Domain's
	// reserved namespace. It is written out rather than drawn from
	// tracking.NewPseudonym because what is under test is what survives a token,
	// not how a pseudonym is generated.
	testPseudonym = "0f8b9d3c2a1e4f5b6c7d8e9f0a1b2c3d@track." + testDomain
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

// answer is what a recipient observes from a tracking request: the status, and
// where a tracked link sent them.
type answer struct {
	status   int
	location string
}

// engage sends one tracking request through the tracker's routing table, with the
// headers a real recipient's client would carry, and returns what the recipient
// observes together with everything the tracker published for it.
func engage(t *testing.T, path string) (answer, []*statstypes.Stats) {
	t.Helper()

	pub := &fakePublisher{}
	srv := newServer(pub, ss, Config{})

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("X-Real-Ip", testIP)

	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	return answer{status: rec.Code, location: rec.Header().Get("Location")}, pub.published()
}

// TestOpenRetainsOnlyWhatItsModeAllows is the compliance property for opens:
// Anonymous names nobody and keeps nothing about the request, Pseudonymous names
// the Delivery's pseudonym and nothing about the request, Identified attributes
// the event to the Recipient and still keeps nothing about the request, and Full
// additionally keeps the IP address and user agent.
func TestOpenRetainsOnlyWhatItsModeAllows(t *testing.T) {
	for _, tc := range retentionCases() {
		t.Run(tc.name, func(t *testing.T) {
			token, err := ss.CreateOpenToken(t.Context(), testMessageID, tc.mint, tc.mode)
			require.NoError(t, err)

			resp, published := engage(t, "/o/"+token)
			assert.Equal(t, http.StatusOK, resp.status)

			require.Len(t, published, 1)
			stat := published[0]
			assert.Equal(t, tc.wantEmail, stat.Email)
			assert.Equal(t, testMessageID, stat.MessageId, "the Batch is named under every Mode")
			assert.Equal(t, testDomain, stat.Domain, "the Domain is named under every Mode")
			assert.Equal(t, tc.wantWireMode, stat.TrackingMode, "the event must carry the resolved Mode")

			opened := stat.Data.GetOpened()
			require.NotNil(t, opened)
			assert.Equal(t, tc.wantIP, opened.Ip)
			assert.Equal(t, tc.wantUserAgent, opened.UserAgent)
		})
	}
}

// TestClickRetainsOnlyWhatItsModeAllows is the same property for links, which
// are governed by their own axis of the Policy.
func TestClickRetainsOnlyWhatItsModeAllows(t *testing.T) {
	for _, tc := range retentionCases() {
		t.Run(tc.name, func(t *testing.T) {
			token, err := ss.CreateLinkToken(t.Context(), testMessageID, tc.mint, testLandingURL, tc.mode)
			require.NoError(t, err)

			resp, published := engage(t, "/c/"+token)
			assert.Equal(t, http.StatusTemporaryRedirect, resp.status)
			assert.Equal(t, testLandingURL, resp.location,
				"the redirect is owed to the recipient under every Mode")

			require.Len(t, published, 1)
			stat := published[0]
			assert.Equal(t, tc.wantEmail, stat.Email)
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
	name string
	mode tracking.Mode
	// mint is the identity the Builder hands the mint under this Mode: the
	// Recipient's own address, or — under Pseudonymous — the pseudonym drawn for
	// this Delivery, which is the one Mode whose identity the caller supplies and
	// statssec only validates (ADR 0006).
	mint          string
	wantWireMode  trackingtypes.TrackingMode
	wantEmail     string
	wantIP        string
	wantUserAgent string
}

// retentionCases are the four Modes under which an engagement event exists, each
// with everything the event is allowed to carry. The empty fields are the point:
// Anonymous names nobody at all, Pseudonymous names only the pseudonym its token
// was minted with, and Identified names the Recipient and nothing else.
func retentionCases() []retentionCase {
	return []retentionCase{
		{
			// The real address is handed to the mint and reaches the event under
			// neither name: the mint replaces it with the Domain's sentinel, and the
			// Tracker drops even that.
			name:         "Anonymous",
			mode:         tracking.ModeAnonymous,
			mint:         testRecipient,
			wantWireMode: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
		},
		{
			// The pseudonym does survive, and it is all that survives — that is the
			// difference between this rung and Anonymous, and the events of a Batch
			// are linkable to each other by nothing else.
			name:         "Pseudonymous",
			mode:         tracking.ModePseudonymous,
			mint:         testPseudonym,
			wantWireMode: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
			wantEmail:    testPseudonym,
		},
		{
			name:         "Identified",
			mode:         tracking.ModeIdentified,
			mint:         testRecipient,
			wantWireMode: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
			wantEmail:    testRecipient,
		},
		{
			name:          "Full",
			mode:          tracking.ModeFull,
			mint:          testRecipient,
			wantWireMode:  trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			wantEmail:     testRecipient,
			wantIP:        testIP,
			wantUserAgent: testUserAgent,
		},
	}
}

// TestAnonymousEventsAreIndistinguishable is the aggregate-only property as an
// operator would meet it: two Recipients of a Batch engaging under Anonymous
// produce two events with nothing in them that tells the two apart. The tokens
// are minted for two different addresses on purpose — the point is that the
// address does not survive.
func TestAnonymousEventsAreIndistinguishable(t *testing.T) {
	firstToken, err := ss.CreateOpenToken(t.Context(), testMessageID, "first@example.com", tracking.ModeAnonymous)
	require.NoError(t, err)
	secondToken, err := ss.CreateOpenToken(t.Context(), testMessageID, "second@example.com", tracking.ModeAnonymous)
	require.NoError(t, err)

	_, first := engage(t, "/o/"+firstToken)
	require.Len(t, first, 1)
	_, second := engage(t, "/o/"+secondToken)
	require.Len(t, second, 1)

	assert.Empty(t, first[0].Email, "an anonymous open must name nobody")
	assert.Empty(t, second[0].Email, "an anonymous open must name nobody")
	assert.Equal(t, first[0].MessageId, second[0].MessageId,
		"an anonymous open is attributed to its Batch and nothing finer")
	assert.Equal(t, first[0].Domain, second[0].Domain)
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
			assert.Equal(t, http.StatusNotFound, resp.status, "a forged token must be refused")
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

// TestRetainedIsGatedOnMode states in one place the rule both handlers share, over
// the whole scale rather than only the Modes a token can currently be minted with.
//
// The line is Pseudonymous, not Identified: from that rung up the event keeps
// whatever identity its token claims, because an event that dropped its pseudonym
// would be linkable to nothing and the rung would record nothing at all. Below it
// the claim is dropped whatever it says, which is why the Modes that keep nothing
// are given a real address to drop: that is what a token minted by an older build
// carries, and the Tracker must not let it through even though the mint no longer
// produces one.
func TestRetainedIsGatedOnMode(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/o/tok", nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("X-Real-Ip", testIP)

	cases := []struct {
		name    string
		mode    tracking.Mode
		claimed string
		want    engagement
	}{
		{name: "Off", mode: tracking.ModeOff, claimed: testRecipient, want: engagement{}},
		{name: "Anonymous", mode: tracking.ModeAnonymous, claimed: testRecipient, want: engagement{}},
		// The sentinel a current token carries under Anonymous is dropped on the
		// same terms: what it stands for is already said by the Mode.
		{
			name:    "AnonymousWithSentinelIdentity",
			mode:    tracking.ModeAnonymous,
			claimed: tracking.AnonymousIdentity(testDomain),
			want:    engagement{},
		},
		{
			name:    "Pseudonymous",
			mode:    tracking.ModePseudonymous,
			claimed: testPseudonym,
			want:    engagement{email: testPseudonym},
		},
		// Keeping the claim cannot invent one. A token minted before the identity
		// claim was always email-shaped left it empty under the Modes that name
		// nobody, and such tokens keep arriving for one token lifetime after this
		// change.
		{
			name:    "PseudonymousFromALegacyTokenWithNoIdentity",
			mode:    tracking.ModePseudonymous,
			claimed: "",
			want:    engagement{},
		},
		{
			name:    "Identified",
			mode:    tracking.ModeIdentified,
			claimed: testRecipient,
			want:    engagement{email: testRecipient},
		},
		{
			name:    "Full",
			mode:    tracking.ModeFull,
			claimed: testRecipient,
			want:    engagement{email: testRecipient, ip: testIP, userAgent: testUserAgent},
		},
		// An unstated Mode imposes no restriction of its own, so it keeps the
		// attribution its token was minted to carry — and still never reaches Full.
		{
			name:    "Unspecified",
			mode:    tracking.ModeUnspecified,
			claimed: testRecipient,
			want:    engagement{email: testRecipient},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, retained(req, tc.claimed, tc.mode))
		})
	}
}

// TestATokenReplayedOnTheOtherChannelIsRefused is the observable half of the
// channel binding. A link token carries the Mode governing *links*, so replaying
// one against the open endpoint would apply the more permissive of a Domain's two
// axes to both: on `opens=off, links=full` it would record an identified open,
// with the requester's IP, for a Domain that asked for no open tracking at all.
// The tokens here are genuine and unmodified — only the endpoint is wrong.
func TestATokenReplayedOnTheOtherChannelIsRefused(t *testing.T) {
	openToken, err := ss.CreateOpenToken(t.Context(), testMessageID, testRecipient, tracking.ModeFull)
	require.NoError(t, err)
	linkToken, err := ss.CreateLinkToken(t.Context(), testMessageID, testRecipient, testLandingURL, tracking.ModeFull)
	require.NoError(t, err)

	cases := []struct {
		name string
		path string
	}{
		{name: "LinkTokenOnTheOpenEndpoint", path: "/o/" + linkToken},
		{name: "OpenTokenOnTheClickEndpoint", path: "/c/" + openToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, published := engage(t, tc.path)
			assert.Equal(t, http.StatusNotFound, resp.status, "a token minted for the other channel must be refused")
			assert.Empty(t, published, "a refused request must publish no stat")
		})
	}
}
