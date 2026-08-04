package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With the audit trail off — the default, and therefore what an upgrade leaves every existing
// deployment with — the API installs no Recorder at all. This is the whole of what "disabled" means
// (ADR 0010): not collected and then discarded, but never collected. Guard keeps the logging Recorder
// it has always had, and this process does not acquire the NATS dependency it otherwise has none of,
// so neither its network requirements nor its failure modes change because the feature exists.
//
// The container is nil on purpose: reaching for one is the failure this test exists to catch.
func TestNoRecorderIsInstalledWhenTheAuditTrailIsOff(t *testing.T) {
	viper.Set("audit.enabled", false)
	t.Cleanup(func() { viper.Set("audit.enabled", nil) })

	assert.Nil(t, startAuditRecording(t.Context(), nil))
}

// spyRecorder counts what it is told and writes nothing, so that a test can tell a decision that
// reached the installed Recorder from one that fell through to the logging default.
type spyRecorder struct {
	decisions []authz.Decision
}

func (s *spyRecorder) Record(d authz.Decision) {
	s.decisions = append(s.decisions, d)
}

// The Recorder reaches Guard through the request's context, however deep into a handler the call goes.
// Asserted over a real ServeHTTP because the middleware is mounted on the whole mux — the Mailer API
// included, that being the one surface the admin token does not authenticate, and the one an
// interceptor hung off its handler options would miss on every send.
func TestAnInstalledRecorderReachesAGuardedOperation(t *testing.T) {
	logged := captureSlog(t)
	spy := &spyRecorder{}

	withRecorder(spy, guardedHandler(t)).ServeHTTP(httptest.NewRecorder(), authenticatedRequest())

	require.Len(t, spy.decisions, 1)
	assert.Equal(t, authz.Allowed, spy.decisions[0].Outcome)
	assert.Equal(t, authz.List, spy.decisions[0].Action)

	// The spy is not a decorator, so nothing was logged. That is what proves the decision went to
	// the Recorder the middleware installed rather than to the default beside it.
	assert.NotContains(t, logged.String(), "RBAC check")
}

// And with nothing installed the same decision goes to the log, unchanged. The pair with the test
// above: between them they say that where a record goes is a wiring decision, and that the wiring
// nobody configured is the behaviour Kannon has always had.
func TestADecisionGoesToTheLogWhenNothingIsInstalled(t *testing.T) {
	logged := captureSlog(t)

	withRecorder(nil, guardedHandler(t)).ServeHTTP(httptest.NewRecorder(), authenticatedRequest())

	assert.Contains(t, logged.String(), `level=DEBUG msg="RBAC check"`)
}

// guardedHandler stands in for any handler that reaches a domain service: it performs one authorized
// operation, which is all the middleware is being asked to have made possible.
func guardedHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		_, err := authz.Guard(req.Context(), authz.List, authz.Domains(), func() (struct{}, error) {
			return struct{}{}, nil
		})
		require.NoError(t, err)
	})
}

// authenticatedRequest carries the Principal an admin token resolves to, so that the guarded operation
// is permitted and the decision recorded is an ordinary one rather than a refusal.
func authenticatedRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return req.WithContext(authz.NewContext(req.Context(), admintoken.AdminPrincipal()))
}

// captureSlog redirects the default logger for the duration of one test, at debug so that the level a
// line was written at is observable rather than filtered out.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}
