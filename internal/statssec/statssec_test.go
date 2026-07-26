package statssec_test

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var s statssec.StatsService

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	q = sqlc.New(db)
	s = statssec.NewStatsService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestCreateOpenToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateOpenToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", tracking.ModeIdentified)
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VerifyOpenToken(t.Context(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
	assert.Equal(t, tracking.ModeIdentified, c.Mode)
}

func TestCreateLinkToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateLinkToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", "https://test.com", tracking.ModeIdentified)
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VerifyLinkToken(t.Context(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
	assert.Equal(t, "https://test.com", c.URL)
	assert.Equal(t, tracking.ModeIdentified, c.Mode)
}

// TestTokensCarryTheMintedMode pins the Mode as a signed claim on both axes: the
// Tracker reads what the Builder minted, whichever Mode that was.
func TestTokensCarryTheMintedMode(t *testing.T) {
	for _, mode := range []tracking.Mode{tracking.ModeIdentified, tracking.ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			openToken, err := s.CreateOpenToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", mode)
			assert.Nil(t, err)
			openClaims, err := s.VerifyOpenToken(t.Context(), openToken)
			assert.Nil(t, err)
			assert.Equal(t, mode, openClaims.Mode)

			linkToken, err := s.CreateLinkToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", "https://test.com", mode)
			assert.Nil(t, err)
			linkClaims, err := s.VerifyLinkToken(t.Context(), linkToken)
			assert.Nil(t, err)
			assert.Equal(t, mode, linkClaims.Mode)
		})
	}
}

// TestTokensCarryNoIdentityWhenTheModeDoesNotIdentify is the Anonymous privacy
// property at the mint: whatever address the caller hands over, an Anonymous
// token names nobody. It is asserted through Verify, so the claim a Tracker will
// actually read is the one under test — and the two tokens are compared to each
// other, because "the claims look the same" is weaker than "the Recipients cannot
// be told apart".
func TestTokensCarryNoIdentityWhenTheModeDoesNotIdentify(t *testing.T) {
	const messageID = "<xxxx/test@test.com>"
	const url = "https://test.com"

	for _, mode := range []tracking.Mode{tracking.ModeAnonymous, tracking.ModePseudonymous} {
		t.Run(string(mode), func(t *testing.T) {
			openToken, err := s.CreateOpenToken(t.Context(), messageID, "first@test.com", mode)
			assert.Nil(t, err)
			openClaims, err := s.VerifyOpenToken(t.Context(), openToken)
			assert.Nil(t, err)
			assert.Empty(t, openClaims.Email, "an open token under %q must name nobody", mode)
			assert.Equal(t, messageID, openClaims.MessageID, "the Batch is still named")
			assert.Equal(t, mode, openClaims.Mode)

			otherClaims, err := s.VerifyOpenToken(t.Context(),
				mustCreateOpenToken(t, messageID, "second@test.com", mode))
			assert.Nil(t, err)
			assert.Equal(t, openClaims.Email, otherClaims.Email,
				"two Recipients of a Batch must be indistinguishable under %q", mode)

			linkToken, err := s.CreateLinkToken(t.Context(), messageID, "first@test.com", url, mode)
			assert.Nil(t, err)
			linkClaims, err := s.VerifyLinkToken(t.Context(), linkToken)
			assert.Nil(t, err)
			assert.Empty(t, linkClaims.Email, "a link token under %q must name nobody", mode)
			assert.Equal(t, url, linkClaims.URL, "the tracked URL is still named")
			assert.Equal(t, mode, linkClaims.Mode)
		})
	}
}

func mustCreateOpenToken(t *testing.T, messageID, email string, mode tracking.Mode) string {
	t.Helper()
	token, err := s.CreateOpenToken(t.Context(), messageID, email, mode)
	assert.Nil(t, err)
	return token
}

// TestTamperedModeClaimIsRefused covers the reason the Mode is signed at all: a
// recipient rewriting their own Mode to escalate what is retained about them must
// not get a verified token out of it.
func TestTamperedModeClaimIsRefused(t *testing.T) {
	token, err := s.CreateOpenToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", tracking.ModeIdentified)
	assert.Nil(t, err)

	tampered := tamperModeClaim(t, token, tracking.ModeFull)
	assert.NotEqual(t, token, tampered, "the tampered token must differ from the original")

	_, err = s.VerifyOpenToken(t.Context(), tampered)
	assert.NotNil(t, err, "a token whose Mode claim was rewritten must not verify")
}

// tamperModeClaim rewrites the Mode of an already-signed token's payload,
// leaving the original header and signature in place — the cheapest attack a
// recipient holding their own token can mount.
func tamperModeClaim(t *testing.T, token string, mode tracking.Mode) string {
	t.Helper()
	parts := strings.Split(token, ".")
	assert.Len(t, parts, 3)

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	assert.Nil(t, err)

	var claims map[string]any
	assert.Nil(t, json.Unmarshal(raw, &claims))
	claims["mode"] = string(mode)

	rewritten, err := json.Marshal(claims)
	assert.Nil(t, err)

	parts[1] = base64.RawURLEncoding.EncodeToString(rewritten)
	return strings.Join(parts, ".")
}
