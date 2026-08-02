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

// batchID is a Batch id in the shape the Builder actually hands over,
// `msg_<cuid>@<fqdn>` — the mint reads the Domain out of it to build the reserved
// namespace, so a test id has to spell one out.
const batchID = "msg_ck7m2n1p0000@test.com"

// TestAnonymousTokensNameNobody is the Anonymous privacy property at the mint:
// whatever address the caller hands over, an Anonymous token carries the Domain's
// sentinel instead. It is asserted through Verify, so the claim a Tracker will
// actually read is the one under test — and the two tokens are compared to each
// other, because "the claims look the same" is weaker than "the Recipients cannot
// be told apart".
func TestAnonymousTokensNameNobody(t *testing.T) {
	const url = "https://test.com"
	const sentinel = "anonymous@track.test.com"

	openClaims, err := s.VerifyOpenToken(t.Context(),
		mustCreateOpenToken(t, batchID, "first@test.com", tracking.ModeAnonymous))
	assert.Nil(t, err)
	assert.Equal(t, sentinel, openClaims.Email, "an Anonymous open token must name nobody")
	assert.Equal(t, batchID, openClaims.MessageID, "the Batch is still named")
	assert.Equal(t, tracking.ModeAnonymous, openClaims.Mode)

	otherClaims, err := s.VerifyOpenToken(t.Context(),
		mustCreateOpenToken(t, batchID, "second@test.com", tracking.ModeAnonymous))
	assert.Nil(t, err)
	assert.Equal(t, openClaims.Email, otherClaims.Email,
		"two Recipients of a Batch must be indistinguishable under Anonymous")

	linkToken, err := s.CreateLinkToken(t.Context(), batchID, "first@test.com", url, tracking.ModeAnonymous)
	assert.Nil(t, err)
	linkClaims, err := s.VerifyLinkToken(t.Context(), linkToken)
	assert.Nil(t, err)
	assert.Equal(t, sentinel, linkClaims.Email, "an Anonymous link token must name nobody")
	assert.Equal(t, url, linkClaims.URL, "the tracked URL is still named")
	assert.Equal(t, tracking.ModeAnonymous, linkClaims.Mode)
}

// TestPseudonymousTokensCarryTheCallersSentinel pins what the mint does with the
// one Mode whose identity it cannot invent for itself: it passes the Builder's
// per-Delivery pseudonym through unchanged, so the pixel and every link of one
// Delivery agree, and it refuses anything outside the Domain's reserved namespace.
//
// The refusal is the chokepoint property (ADR 0006): a caller that passes the real
// address with a Pseudonymous Mode is caught here rather than shipped.
func TestPseudonymousTokensCarryTheCallersSentinel(t *testing.T) {
	const pseudonym = "0123456789abcdef0123456789abcdef@track.test.com"
	const url = "https://test.com"

	t.Run("A sentinel of the Batch's Domain is minted", func(t *testing.T) {
		openClaims, err := s.VerifyOpenToken(t.Context(),
			mustCreateOpenToken(t, batchID, pseudonym, tracking.ModePseudonymous))
		assert.Nil(t, err)
		assert.Equal(t, pseudonym, openClaims.Email)
		assert.Equal(t, tracking.ModePseudonymous, openClaims.Mode)

		linkToken, err := s.CreateLinkToken(t.Context(), batchID, pseudonym, url, tracking.ModePseudonymous)
		assert.Nil(t, err)
		linkClaims, err := s.VerifyLinkToken(t.Context(), linkToken)
		assert.Nil(t, err)
		assert.Equal(t, pseudonym, linkClaims.Email,
			"the pixel and the links of one Delivery must carry the same pseudonym")
	})

	outside := []struct {
		name     string
		identity string
	}{
		{name: "the recipient's real address", identity: "first@test.com"},
		{name: "nothing at all", identity: ""},
		{name: "another Domain's namespace", identity: "abc@track.other.com"},
		{name: "the bare sending Domain", identity: "abc@test.com"},
		// In the namespace, but the one address the Stats worker reads as naming
		// nobody: minting it would produce an event that is dropped downstream as
		// an upstream bug.
		{name: "the anonymous sentinel", identity: "anonymous@track.test.com"},
	}

	for _, tc := range outside {
		t.Run("A Pseudonymous mint is refused for "+tc.name, func(t *testing.T) {
			_, err := s.CreateOpenToken(t.Context(), batchID, tc.identity, tracking.ModePseudonymous)
			assert.ErrorIs(t, err, statssec.ErrIdentityOutsideNamespace)

			_, err = s.CreateLinkToken(t.Context(), batchID, tc.identity, url, tracking.ModePseudonymous)
			assert.ErrorIs(t, err, statssec.ErrIdentityOutsideNamespace)
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

// TestATokenIsBoundToItsChannel covers the one way the Tracking Mode could be
// applied to a channel it was not signed for. The two claim shapes differ only by
// a field JSON parsing ignores when absent, so before the audience was checked a
// link token verified cleanly as open claims and handed over the Mode governing
// *links* — letting a Domain on `opens=off, links=full` be made to record an
// identified open with the requester's IP.
func TestATokenIsBoundToItsChannel(t *testing.T) {
	const messageID = "<xxxx/test@test.com>"
	const email = "test@test.com"

	openToken, err := s.CreateOpenToken(t.Context(), messageID, email, tracking.ModeIdentified)
	assert.Nil(t, err)
	linkToken, err := s.CreateLinkToken(t.Context(), messageID, email, "https://example.com", tracking.ModeFull)
	assert.Nil(t, err)

	t.Run("A link token is refused as an open", func(t *testing.T) {
		_, err := s.VerifyOpenToken(t.Context(), linkToken)
		assert.NotNil(t, err, "a link token must not verify as an open token")
	})

	t.Run("An open token is refused as a click", func(t *testing.T) {
		_, err := s.VerifyLinkToken(t.Context(), openToken)
		assert.NotNil(t, err, "an open token must not verify as a link token")
	})

	t.Run("Each verifies on its own channel", func(t *testing.T) {
		open, err := s.VerifyOpenToken(t.Context(), openToken)
		assert.Nil(t, err)
		assert.Equal(t, tracking.ModeIdentified, open.Mode)

		link, err := s.VerifyLinkToken(t.Context(), linkToken)
		assert.Nil(t, err)
		assert.Equal(t, tracking.ModeFull, link.Mode)
	})
}
