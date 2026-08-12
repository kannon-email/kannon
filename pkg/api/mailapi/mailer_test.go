package mailapi_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	mailerv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	types "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
)

func TestSendMail_RejectsCrossDomainSender(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "ceo@other-tenant.com",
			Alias: "CEO",
		},
		Recipients: []*types.Recipient{
			{Email: "victim@example.com"},
		},
		Subject:       "Spoofed",
		Html:          "<p>hi</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, d)

	_, err := ts.SendHTML(t.Context(), req)
	assert.NotNil(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	// The refusal is the guard's now, and the wording is asserted literally: a
	// client that branches on the code or matches the message must not be able to
	// tell that the mechanism behind it changed (#438).
	assert.Equal(t, fmt.Sprintf("permission_denied: sender domain %q is not authorized for tenant %q",
		"other-tenant.com", d.Domain.Domain), err.Error())
}

// TestSendMail_RejectsSenderOfAnotherRegisteredDomain is the cross-Domain refusal with both Domains
// real: a key reaches its own Domain's Batches and nothing else, DKIM keypair or not. It also
// asserts the refusal leaves nothing behind, since the guard wraps the whole of the send.
func TestSendMail_RejectsSenderOfAnotherRegisteredDomain(t *testing.T) {
	defer cleanDB(t)

	mine := createTestDomain(t)
	theirs := createTestDomain(t)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "ceo@" + theirs.Domain.Domain,
			Alias: "CEO",
		},
		Recipients: []*types.Recipient{
			{Email: "victim@example.com"},
		},
		Subject:       "Spoofed",
		Html:          "<p>hi</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, mine)

	_, err := ts.SendHTML(t.Context(), req)
	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.Equal(t, fmt.Sprintf("permission_denied: sender domain %q is not authorized for tenant %q",
		theirs.Domain.Domain, mine.Domain.Domain), err.Error())

	var msgCount int
	err = db.QueryRow(t.Context(), "SELECT count(*) FROM messages").Scan(&msgCount)
	require.NoError(t, err)
	assert.Equal(t, 0, msgCount, "a refused send must not create a Batch")
}

// TestSendMail_RefusesWithoutCredential covers the surface the Mailer API has always had and keeps:
// nothing in front of it hands it an authority, so a request carrying no credential resolves to no
// Principal and no Domain, and never reaches a guard at all.
func TestSendMail_RefusesWithoutCredential(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	newReq := func() *connect.Request[mailerv1.SendHTMLReq] {
		return connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender: &types.Sender{
				Email: "test@" + d.Domain.Domain,
				Alias: "Test",
			},
			Recipients:    []*types.Recipient{{Email: "recipient@example.com"}},
			Subject:       "Test",
			Html:          "<p>Hello</p>",
			ScheduledTime: timestamppb.Now(),
		})
	}

	t.Run("no Authorization header", func(t *testing.T) {
		_, err := ts.SendHTML(t.Context(), newReq())
		require.Error(t, err)
		assert.Equal(t, "invalid or wrong auth", err.Error())
	})

	t.Run("wrong key for a real Domain", func(t *testing.T) {
		req := newReq()
		req.Header().Set("Authorization", "Basic "+
			base64.StdEncoding.EncodeToString([]byte(d.Domain.Domain+":k_wrong12345678901234567890")))

		_, err := ts.SendHTML(t.Context(), req)
		require.Error(t, err)
		assert.Equal(t, "invalid or wrong auth", err.Error())
	})
}

// TestSendMail_IgnoresAnAdminPrincipalInTheContext is the assertion behind the one
// security-relevant line of pkg/api/api.go: the Mailer handler is mounted without the admin-token
// options, and planting an admin Principal anyway changes nothing — authentication replaces it.
func TestSendMail_IgnoresAnAdminPrincipalInTheContext(t *testing.T) {
	defer cleanDB(t)

	mine := createTestDomain(t)
	theirs := createTestDomain(t)
	adminCtx := tests.AdminContext(t.Context())

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "ceo@" + theirs.Domain.Domain,
			Alias: "CEO",
		},
		Recipients:    []*types.Recipient{{Email: "victim@example.com"}},
		Subject:       "Spoofed",
		Html:          "<p>hi</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, mine)

	_, err := ts.SendHTML(adminCtx, req)
	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.Equal(t, fmt.Sprintf("permission_denied: sender domain %q is not authorized for tenant %q",
		theirs.Domain.Domain, mine.Domain.Domain), err.Error())

	unauthenticated := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender:        &types.Sender{Email: "ceo@" + mine.Domain.Domain, Alias: "CEO"},
		Recipients:    []*types.Recipient{{Email: "victim@example.com"}},
		Subject:       "Spoofed",
		Html:          "<p>hi</p>",
		ScheduledTime: timestamppb.Now(),
	})

	_, err = ts.SendHTML(adminCtx, unauthenticated)
	require.Error(t, err)
	assert.Equal(t, "invalid or wrong auth", err.Error())
}

func TestSendMail_AcceptsSenderFromParentDomain(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	// Test domains look like "<cuid>.example.com"; authenticated as that
	// subdomain, the tenant must still be able to send from the parent
	// "@example.com" address.
	parent := d.Domain.Domain[strings.Index(d.Domain.Domain, ".")+1:]

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "ludovico@" + parent,
			Alias: "Ludovico",
		},
		Recipients: []*types.Recipient{
			{Email: "recipient@example.com"},
		},
		Subject:       "Test",
		Html:          "<p>Hello</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)
	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
}

func TestSendMail_RejectsLookalikeParentDomain(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	// Tenant is "<cuid>.example.com"; sending from "@notexample.com" must be
	// rejected — the parent-domain relaxation must not match by raw substring.
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "evil@not" + d.Domain.Domain[strings.Index(d.Domain.Domain, ".")+1:],
			Alias: "Evil",
		},
		Recipients: []*types.Recipient{
			{Email: "victim@example.com"},
		},
		Subject:       "Spoofed",
		Html:          "<p>hi</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, d)

	_, err := ts.SendHTML(t.Context(), req)
	assert.NotNil(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestSendMail_RejectsCRLFInputs(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)
	good := "test@" + d.Domain.Domain

	tests := []struct {
		name   string
		mutate func(req *mailerv1.SendHTMLReq)
	}{
		{
			name: "CRLF in sender email",
			mutate: func(req *mailerv1.SendHTMLReq) {
				req.Sender.Email = "a@" + d.Domain.Domain + "\r\nBcc: evil@x.com"
			},
		},
		{
			name: "CRLF in sender alias",
			mutate: func(req *mailerv1.SendHTMLReq) {
				req.Sender.Alias = "Bob\r\nBcc: evil@x.com"
			},
		},
		{
			name: "CRLF in subject",
			mutate: func(req *mailerv1.SendHTMLReq) {
				req.Subject = "Hi\r\nBcc: evil@x.com"
			},
		},
		{
			name: "CRLF in custom To header",
			mutate: func(req *mailerv1.SendHTMLReq) {
				req.Headers = &types.Headers{
					To: []string{"a@b.com\r\nBcc: evil@x.com"},
				}
			},
		},
		{
			name: "CRLF in custom Cc header",
			mutate: func(req *mailerv1.SendHTMLReq) {
				req.Headers = &types.Headers{
					Cc: []string{"a@b.com\r\nBcc: evil@x.com"},
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := connect.NewRequest(&mailerv1.SendHTMLReq{
				Sender: &types.Sender{
					Email: good,
					Alias: "Test",
				},
				Recipients: []*types.Recipient{
					{Email: "recipient@example.com"},
				},
				Subject:       "Test",
				Html:          "<p>Hello</p>",
				ScheduledTime: timestamppb.Now(),
			})
			tc.mutate(req.Msg)
			authRequest(req, d)

			_, err := ts.SendHTML(t.Context(), req)
			assert.NotNil(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

func TestSendMail_AcceptsValidAliasWithApostrophe(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "O'Brien Mailing",
		},
		Recipients: []*types.Recipient{
			{Email: "recipient@example.com"},
		},
		Subject:       "Test",
		Html:          "<p>Hello</p>",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)
	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
}

func TestInsertMail(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Recipients: []*types.Recipient{
			{
				Email: "test@email.com",
				Fields: map[string]string{
					"name": "Test",
				},
			},
		},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.New(schedTime),
	})

	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)
	assert.True(t, strings.HasSuffix(res.Msg.MessageId, "@"+d.Domain.Domain))
	assert.True(t, strings.HasSuffix(res.Msg.TemplateId, "@"+d.Domain.Domain))

	sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: res.Msg.MessageId,
		Limit:     100,
		Offset:    0,
	})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(sp))
	assert.Equal(t, "test@email.com", sp[0].Email)
	assert.Equal(t, sqlc.SendingPoolStatusToValidate, sp[0].Status)
	assert.Equal(t, "Test", sp[0].Fields["name"])

	assert.Equal(t, schedTime.UTC(), sp[0].ScheduledTime.Time.UTC())
}

// TestSendMailFreezesResolvedTrackingPolicy asserts what a caller can observe
// after a send: the Policy on every Delivery is the concrete result of resolving
// the Domain's ceiling at intake, never a Policy that states nothing.
func TestSendMailFreezesResolvedTrackingPolicy(t *testing.T) {
	cases := []struct {
		name   string
		domain *trackingtypes.TrackingPolicy
		want   tracking.Policy
	}{
		{
			name: "DomainDefault",
			want: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
		},
		{
			name: "DomainOff",
			domain: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
			want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
		},
		{
			name: "DomainOffForOpensOnly",
			domain: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
			},
			want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeIdentified},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer cleanDB(t)

			d := createTestDomain(t)

			if tc.domain != nil {
				_, err := adminAPI.SetTrackingPolicy(tests.AdminContext(t.Context()), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
					Domain:   d.Domain.Domain,
					Tracking: tc.domain,
				}))
				assert.Nil(t, err)
			}

			req := connect.NewRequest(&mailerv1.SendHTMLReq{
				Sender: &types.Sender{
					Email: "test@" + d.Domain.Domain,
					Alias: "Test",
				},
				Recipients: []*types.Recipient{
					{Email: "first@email.com"},
					{Email: "second@email.com"},
				},
				Subject:       "Test",
				Html:          "<p>Hello</p>",
				ScheduledTime: timestamppb.Now(),
			})
			authRequest(req, d)

			res, err := ts.SendHTML(t.Context(), req)
			assert.Nil(t, err)

			sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
				MessageID: res.Msg.MessageId,
				Limit:     100,
				Offset:    0,
			})
			assert.Nil(t, err)
			assert.Equal(t, 2, len(sp))
			for _, row := range sp {
				assert.Equal(t, tc.want, row.Tracking, "delivery %s", row.Email)
			}
		})
	}
}

// TestSendMailBatchTrackingPolicy covers the Batch level of the Tracking Policy cascade (#417): at
// or below the Domain's ceiling it resolves onto every Delivery and stays on the Batch row as
// provenance; above it the call fails naming the axis (ADR 0003), as it does for an unreadable Mode.
func TestSendMailBatchTrackingPolicy(t *testing.T) {
	t.Run("AtOrBelowCeilingIsAcceptedAndApplied", func(t *testing.T) {
		cases := []struct {
			name  string
			batch *trackingtypes.TrackingPolicy
			want  tracking.Policy
		}{
			{
				name: "EqualToCeiling",
				batch: &trackingtypes.TrackingPolicy{
					Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
					Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
				},
				want: tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			},
			{
				name: "BelowCeiling",
				batch: &trackingtypes.TrackingPolicy{
					Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
					Links: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
				},
				want: tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeAnonymous},
			},
			{
				// Pseudonymous sits between Anonymous and Identified (#424,
				// ADR 0006), so it is below the default ceiling and travels the
				// cascade like every other rung — no longer refused as reserved.
				name: "Pseudonymous",
				batch: &trackingtypes.TrackingPolicy{
					Opens: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
					Links: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
				},
				want: tracking.Policy{Opens: tracking.ModePseudonymous, Links: tracking.ModePseudonymous},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer cleanDB(t)

				// createTestDomain leaves the Domain at its identified/identified default.
				d := createTestDomain(t)

				req := connect.NewRequest(&mailerv1.SendHTMLReq{
					Sender: &types.Sender{
						Email: "test@" + d.Domain.Domain,
						Alias: "Test",
					},
					Recipients:    []*types.Recipient{{Email: "first@email.com"}},
					Subject:       "Test",
					Html:          "<p>Hello</p>",
					ScheduledTime: timestamppb.Now(),
					Tracking:      tc.batch,
				})
				authRequest(req, d)

				res, err := ts.SendHTML(t.Context(), req)
				assert.Nil(t, err)

				sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
					MessageID: res.Msg.MessageId,
					Limit:     100,
					Offset:    0,
				})
				assert.Nil(t, err)
				assert.Equal(t, 1, len(sp))
				assert.Equal(t, tc.want, sp[0].Tracking, "the resolved Policy must be frozen on the Delivery")

				msg, err := q.GetMessage(t.Context(), res.Msg.MessageId)
				assert.Nil(t, err)
				assert.Equal(t, tc.want, msg.Tracking, "the stated Policy must be kept verbatim on the Batch as provenance")
			})
		}
	})

	t.Run("AboveCeilingFailsTheCall", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)
		_, err := adminAPI.SetTrackingPolicy(tests.AdminContext(t.Context()), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
			Domain: d.Domain.Domain,
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}))
		assert.Nil(t, err)

		req := connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender: &types.Sender{
				Email: "test@" + d.Domain.Domain,
				Alias: "Test",
			},
			Recipients:    []*types.Recipient{{Email: "first@email.com"}},
			Subject:       "Test",
			Html:          "<p>Hello</p>",
			ScheduledTime: timestamppb.Now(),
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			},
		})
		authRequest(req, d)

		_, err = ts.SendHTML(t.Context(), req)
		assert.NotNil(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "opens")

		// The ceiling is checked before the Batch is persisted or any Delivery
		// scheduled: a rejected call must leave nothing behind.
		var msgCount int
		err = db.QueryRow(t.Context(), "SELECT count(*) FROM messages WHERE domain = $1", d.Domain.Domain).Scan(&msgCount)
		assert.Nil(t, err)
		assert.Equal(t, 0, msgCount, "a Batch violating its Domain's ceiling must not be created")
	})

	t.Run("AxesAreEvaluatedIndependently", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)
		_, err := adminAPI.SetTrackingPolicy(tests.AdminContext(t.Context()), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
			Domain: d.Domain.Domain,
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			},
		}))
		assert.Nil(t, err)

		req := connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender: &types.Sender{
				Email: "test@" + d.Domain.Domain,
				Alias: "Test",
			},
			Recipients:    []*types.Recipient{{Email: "first@email.com"}},
			Subject:       "Test",
			Html:          "<p>Hello</p>",
			ScheduledTime: timestamppb.Now(),
			Tracking: &trackingtypes.TrackingPolicy{
				// Opens asks for less than its ceiling: fine on its own.
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
				// Links asks for more than its ceiling (off): must violate.
				Links: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			},
		})
		authRequest(req, d)

		_, err = ts.SendHTML(t.Context(), req)
		assert.NotNil(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "links")
		assert.NotContains(t, err.Error(), "opens", "only the violating axis should be named")
	})

	// A wire value this build cannot read is a client built against a newer
	// schema. It is one instruction about the whole send, like a ceiling
	// violation, so it fails the call rather than being read as something else.
	t.Run("AModeFromANewerSchemaFailsTheCall", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)

		req := connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender: &types.Sender{
				Email: "test@" + d.Domain.Domain,
				Alias: "Test",
			},
			Recipients:    []*types.Recipient{{Email: "first@email.com"}},
			Subject:       "Test",
			Html:          "<p>Hello</p>",
			ScheduledTime: timestamppb.Now(),
			Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode(9999),
			},
		})
		authRequest(req, d)

		_, err := ts.SendHTML(t.Context(), req)
		assert.NotNil(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		var msgCount int
		err = db.QueryRow(t.Context(), "SELECT count(*) FROM messages WHERE domain = $1", d.Domain.Domain).Scan(&msgCount)
		assert.Nil(t, err)
		assert.Equal(t, 0, msgCount, "a Batch whose Policy could not be read must not be created")
	})

	t.Run("OmittingTheFieldBehavesExactlyAsBefore", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)

		req := connect.NewRequest(&mailerv1.SendHTMLReq{
			Sender: &types.Sender{
				Email: "test@" + d.Domain.Domain,
				Alias: "Test",
			},
			Recipients:    []*types.Recipient{{Email: "first@email.com"}},
			Subject:       "Test",
			Html:          "<p>Hello</p>",
			ScheduledTime: timestamppb.Now(),
			// Tracking left nil: states nothing, imposes no restriction of
			// its own, resolves to the Domain value.
		})
		authRequest(req, d)

		res, err := ts.SendHTML(t.Context(), req)
		assert.Nil(t, err)

		sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
			MessageID: res.Msg.MessageId,
			Limit:     100,
			Offset:    0,
		})
		assert.Nil(t, err)
		assert.Equal(t, 1, len(sp))
		assert.Equal(t, tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified}, sp[0].Tracking)

		msg, err := q.GetMessage(t.Context(), res.Msg.MessageId)
		assert.Nil(t, err)
		assert.Equal(t, tracking.Policy{}, msg.Tracking,
			"an omitted Batch Policy states nothing and must not be normalised at rest")
	})
}

func TestSendMailWithInvalidHeaders(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	tests := []struct {
		name    string
		headers *types.Headers
		errMsg  string
	}{
		{
			name: "invalid To header",
			headers: &types.Headers{
				To: []string{"not-an-email"},
			},
			errMsg: "invalid To header",
		},
		{
			name: "invalid Cc header",
			headers: &types.Headers{
				Cc: []string{"also-not-valid"},
			},
			errMsg: "invalid Cc header",
		},
		{
			name: "mixed valid and invalid To",
			headers: &types.Headers{
				To: []string{"valid@example.com", "bad"},
			},
			errMsg: "invalid To header",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := connect.NewRequest(&mailerv1.SendHTMLReq{
				Sender: &types.Sender{
					Email: "test@" + d.Domain.Domain,
					Alias: "Test",
				},
				Recipients: []*types.Recipient{
					{Email: "recipient@example.com"},
				},
				Subject:       "Test",
				Html:          "<p>Hello</p>",
				ScheduledTime: timestamppb.Now(),
				Headers:       tc.headers,
			})
			authRequest(req, d)

			_, err := ts.SendHTML(t.Context(), req)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.errMsg)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestSendMailRecipientTrackingPolicy covers the Recipient level of the cascade (#419) — the most
// specific of the three, where a caller states the consent it holds in its own CRM (ADR 0002).
// Everything asserted is observable: the send response, and the Policy frozen on each Delivery.
func TestSendMailRecipientTrackingPolicy(t *testing.T) {
	t.Run("TheCascadeResolvesToTheMostRestrictiveLevel", func(t *testing.T) {
		cases := []struct {
			name      string
			domain    *trackingtypes.TrackingPolicy
			batch     *trackingtypes.TrackingPolicy
			recipient *trackingtypes.TrackingPolicy
			want      tracking.Policy
		}{
			{
				name:      "RecipientOffWinsOverDomainFull",
				domain:    wirePolicy(wireFull, wireFull),
				recipient: wirePolicy(wireOff, wireOff),
				want:      tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			},
			{
				name:      "RecipientNarrowsTheBatch",
				domain:    wirePolicy(wireFull, wireFull),
				batch:     wirePolicy(wireIdentified, wireIdentified),
				recipient: wirePolicy(wireAnonymous, wireAnonymous),
				want:      tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous},
			},
			{
				name:      "TheBatchStillNarrowsARecipientAtTheCeiling",
				domain:    wirePolicy(wireFull, wireFull),
				batch:     wirePolicy(wireOff, wireOff),
				recipient: wirePolicy(wireFull, wireFull),
				want:      tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeOff},
			},
			{
				name:      "AnUnstatedRecipientAxisImposesNoRestriction",
				domain:    wirePolicy(wireFull, wireFull),
				recipient: wirePolicy(wireOff, wireUnspecified),
				want:      tracking.Policy{Opens: tracking.ModeOff, Links: tracking.ModeFull},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer cleanDB(t)

				d := createTestDomain(t)
				setDomainTracking(t, d, tc.domain)

				res := requireSend(t, d, tc.batch, &types.Recipient{
					Email:    "first@email.com",
					Tracking: tc.recipient,
				})

				assert.Empty(t, rejections(t, res))
				assert.EqualValues(t, 1, res.AcceptedCount)
				assert.Equal(t, map[string]tracking.Policy{"first@email.com": tc.want}, frozenPolicies(t, res.MessageId))
			})
		}
	})

	t.Run("EachRecipientOfOneBatchGetsItsOwnPolicy", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)
		setDomainTracking(t, d, wirePolicy(wireFull, wireFull))

		res := requireSend(t, d, nil,
			&types.Recipient{Email: "consented@email.com", Tracking: wirePolicy(wireFull, wireFull)},
			&types.Recipient{Email: "refused@email.com", Tracking: wirePolicy(wireOff, wireOff)},
			&types.Recipient{Email: "unstated@email.com"},
		)

		assert.Empty(t, rejections(t, res))
		assert.EqualValues(t, 3, res.AcceptedCount)
		assert.Equal(t, map[string]tracking.Policy{
			"consented@email.com": {Opens: tracking.ModeFull, Links: tracking.ModeFull},
			"refused@email.com":   {Opens: tracking.ModeOff, Links: tracking.ModeOff},
			"unstated@email.com":  {Opens: tracking.ModeFull, Links: tracking.ModeFull},
		}, frozenPolicies(t, res.MessageId))
	})

	// A Recipient's ceiling is its Domain's Policy, so consent can never widen
	// what the operator authorised (ADR 0003). Exceeding it Rejects the one
	// Recipient and nothing else: one bad row must not fail a send of thousands.
	t.Run("AboveTheDomainCeilingRejectsOnlyThatRecipient", func(t *testing.T) {
		cases := []struct {
			name      string
			domain    *trackingtypes.TrackingPolicy
			recipient *trackingtypes.TrackingPolicy
		}{
			{
				name:      "AboveIdentified",
				domain:    wirePolicy(wireIdentified, wireIdentified),
				recipient: wirePolicy(wireFull, wireFull),
			},
			{
				name:      "RecipientFullCannotWinOverDomainOff",
				domain:    wirePolicy(wireOff, wireOff),
				recipient: wirePolicy(wireFull, wireFull),
			},
			{
				name:      "AboveOnOneAxisOnly",
				domain:    wirePolicy(wireIdentified, wireOff),
				recipient: wirePolicy(wireAnonymous, wireFull),
			},
			{
				// Pseudonymous is honoured now (#424), but it is still a rung of
				// the same scale: asked for above a Domain that allows only
				// aggregate counting, it is refused like any other widening.
				name:      "PseudonymousAboveAnAnonymousCeiling",
				domain:    wirePolicy(wireAnonymous, wireAnonymous),
				recipient: wirePolicy(wirePseudonymous, wireUnspecified),
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				defer cleanDB(t)

				d := createTestDomain(t)
				setDomainTracking(t, d, tc.domain)

				res := requireSend(t, d, nil,
					&types.Recipient{Email: "fine@email.com"},
					&types.Recipient{Email: "greedy@email.com", Tracking: tc.recipient},
					&types.Recipient{Email: "alsofine@email.com"},
				)

				assert.Equal(t, map[string]string{"greedy@email.com": "tracking_above_ceiling"}, rejections(t, res))
				assert.EqualValues(t, 2, res.AcceptedCount)
				assert.ElementsMatch(t, []string{"fine@email.com", "alsofine@email.com"},
					poolEmails(t, res.MessageId),
					"the rest of the Batch must proceed normally")
			})
		}
	})

	// Pseudonymous is a Mode a Recipient may state like any other since #424 (ADR 0006): under
	// the identified/identified default it is a narrowing, so it is accepted and frozen on that
	// Recipient's Delivery alone, leaving the other axis and the other Recipients untouched.
	t.Run("PseudonymousIsAcceptedAndFrozenOnThatRecipient", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)

		res := requireSend(t, d, nil,
			&types.Recipient{Email: "unstated@email.com"},
			&types.Recipient{Email: "pseudonymous@email.com", Tracking: wirePolicy(wirePseudonymous, wireUnspecified)},
		)

		assert.Empty(t, rejections(t, res))
		assert.EqualValues(t, 2, res.AcceptedCount)
		assert.Equal(t, map[string]tracking.Policy{
			"unstated@email.com":     {Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified},
			"pseudonymous@email.com": {Opens: tracking.ModePseudonymous, Links: tracking.ModeIdentified},
		}, frozenPolicies(t, res.MessageId))
	})

	// A Mode this build cannot read is the one place the Recipient level and the
	// Batch level differ in kind: a Batch stating it fails the whole call, a
	// Recipient stating it is Rejected on its own.
	t.Run("AModeFromANewerSchemaRejectsOnlyThatRecipient", func(t *testing.T) {
		defer cleanDB(t)

		requireOnlyBadRowRejected(t, createTestDomain(t),
			&types.Recipient{Email: "unreadable@email.com", Tracking: wirePolicy(trackingtypes.TrackingMode(9999), wireUnspecified)},
			"unsupported_tracking_mode")
	})

	// A row can fail two ways at once, and the caller is told the first one the intake
	// asks about. Pinned because the reason is a token callers branch on, and because
	// translating the Policy earlier than the address is checked would silently change it.
	t.Run("ARowWithNoAddressIsRefusedForTheAddressBeforeItsMode", func(t *testing.T) {
		defer cleanDB(t)

		requireOnlyBadRowRejected(t, createTestDomain(t),
			&types.Recipient{Email: "  ", Tracking: wirePolicy(trackingtypes.TrackingMode(9999), wireUnspecified)},
			"invalid_email")
	})

	t.Run("OmittingTheFieldBehavesExactlyAsBefore", func(t *testing.T) {
		defer cleanDB(t)

		d := createTestDomain(t)

		// No Recipient states anything: every Delivery resolves to the Domain's
		// identified/identified default and nothing is Rejected.
		res := requireSend(t, d, nil,
			&types.Recipient{Email: "first@email.com"},
			&types.Recipient{Email: "second@email.com"},
		)

		assert.Empty(t, rejections(t, res))
		assert.EqualValues(t, 2, res.AcceptedCount)
		assert.NotEmpty(t, res.MessageId)
		assert.NotEmpty(t, res.TemplateId)

		identified := tracking.Policy{Opens: tracking.ModeIdentified, Links: tracking.ModeIdentified}
		assert.Equal(t, map[string]tracking.Policy{
			"first@email.com":  identified,
			"second@email.com": identified,
		}, frozenPolicies(t, res.MessageId))
	})
}

func TestSendMailRejectsRecipientsWithEmptyEmail(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Recipients: []*types.Recipient{
			{Email: "first@email.com"},
			{Email: ""},
			{Email: "second@email.com"},
			{Email: "   "},
			{Email: "third@email.com"},
		},
		Subject:       "Test",
		Html:          "Hello",
		ScheduledTime: timestamppb.New(schedTime),
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)

	// The dropped rows are reported rather than swallowed (#364): a caller can
	// reconcile the 3 it queued against the 2 it did not.
	assert.EqualValues(t, 3, res.Msg.AcceptedCount)
	assert.Equal(t, map[string]string{
		"":    "invalid_email",
		"   ": "invalid_email",
	}, rejections(t, res.Msg))

	assert.ElementsMatch(t, []string{"first@email.com", "second@email.com", "third@email.com"},
		poolEmails(t, res.Msg.MessageId))
}

// TestSendMailWithAllRecipientsRejectedIsNotASilentSuccess closes the data-loss
// hole in #364: a caller whose every Recipient was refused used to get a Batch
// id and no indication that nothing had been queued.
func TestSendMailWithAllRecipientsRejectedIsNotASilentSuccess(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	res := requireSend(t, d, nil,
		&types.Recipient{Email: ""},
		&types.Recipient{Email: "\t"},
	)

	assert.NotEmpty(t, res.MessageId)
	assert.EqualValues(t, 0, res.AcceptedCount)
	assert.EqualValues(t, 2, res.RejectedCount)
	assert.Len(t, res.RejectedRecipients, 2)
	for _, r := range res.RejectedRecipients {
		assert.Equal(t, "invalid_email", r.Reason)
	}
	assert.Empty(t, poolEmails(t, res.MessageId))
}

// Shorthands for the wire enum, so the cascade tables above stay readable.
const (
	wireUnspecified  = trackingtypes.TrackingMode_TRACKING_MODE_UNSPECIFIED
	wireOff          = trackingtypes.TrackingMode_TRACKING_MODE_OFF
	wireAnonymous    = trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS
	wirePseudonymous = trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS
	wireIdentified   = trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED
	wireFull         = trackingtypes.TrackingMode_TRACKING_MODE_FULL
)

func wirePolicy(opens, links trackingtypes.TrackingMode) *trackingtypes.TrackingPolicy {
	return &trackingtypes.TrackingPolicy{Opens: opens, Links: links}
}

// setDomainTracking sets the Domain's Tracking Policy — its ceiling — the way an
// operator would, through the Admin API. A nil Policy leaves the Domain at the
// identified/identified default a fresh Domain starts from.
func setDomainTracking(t *testing.T, d *tests.DomainWithKey, p *trackingtypes.TrackingPolicy) {
	t.Helper()
	if p == nil {
		return
	}
	_, err := adminAPI.SetTrackingPolicy(tests.AdminContext(t.Context()), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
		Domain:   d.Domain.Domain,
		Tracking: p,
	}))
	require.NoError(t, err)
}

// requireOnlyBadRowRejected sends bad alongside one unremarkable Recipient and asserts that exactly
// bad was refused, for wantReason, while the other was accepted and reached the Pool. That second
// half is the point: what distinguishes a per-Recipient rejection from a failed call is the rest of
// the Batch going out, so a test that only checked the reason would pass on a Batch of one.
func requireOnlyBadRowRejected(t *testing.T, d *tests.DomainWithKey, bad *types.Recipient, wantReason string) {
	t.Helper()

	res := requireSend(t, d, nil, &types.Recipient{Email: "fine@email.com"}, bad)

	assert.Equal(t, map[string]string{bad.Email: wantReason}, rejections(t, res))
	assert.EqualValues(t, 1, res.AcceptedCount)
	assert.Equal(t, []string{"fine@email.com"}, poolEmails(t, res.MessageId))
}

// requireSend performs one send for d with an optional Batch-level Policy and the given Recipients,
// returning the response an API caller actually sees. It requires the call itself to succeed, so a
// test asserting a per-Recipient rejection is asserting that the Batch was not failed.
func requireSend(t *testing.T, d *tests.DomainWithKey, batchPolicy *trackingtypes.TrackingPolicy, recipients ...*types.Recipient) *mailerv1.SendRes {
	t.Helper()
	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Recipients:    recipients,
		Subject:       "Test",
		Html:          `<p>Hello</p>`,
		ScheduledTime: timestamppb.Now(),
		Tracking:      batchPolicy,
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)
	require.NoError(t, err)
	return res.Msg
}

// rejections returns the Rejected Recipients of a send as address to reason, and
// checks along the way that the reported count agrees with the list.
func rejections(t *testing.T, res *mailerv1.SendRes) map[string]string {
	t.Helper()
	assert.EqualValues(t, len(res.RejectedRecipients), res.RejectedCount,
		"rejected_count must agree with rejected_recipients")
	out := make(map[string]string, len(res.RejectedRecipients))
	for _, r := range res.RejectedRecipients {
		out[r.Email] = r.Reason
	}
	return out
}

// frozenPolicies returns the Tracking Policy frozen on each Delivery of a Batch,
// keyed by Recipient address. This is the value the Builder acts on, so it is
// the observable consequence of the cascade.
func frozenPolicies(t *testing.T, messageID string) map[string]tracking.Policy {
	t.Helper()
	out := make(map[string]tracking.Policy)
	for _, row := range pool(t, messageID) {
		out[row.Email] = row.Tracking
	}
	return out
}

// poolEmails returns the addresses actually queued for a Batch.
func poolEmails(t *testing.T, messageID string) []string {
	t.Helper()
	rows := pool(t, messageID)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Email)
	}
	return out
}

func pool(t *testing.T, messageID string) []sqlc.SendingPoolEmail {
	t.Helper()
	sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: messageID,
		Limit:     100,
		Offset:    0,
	})
	require.NoError(t, err)
	return sp
}

func TestSendMailWithGlobalFields(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Subject:       "Test",
		Html:          "Hello {{ name }}",
		ScheduledTime: timestamppb.New(schedTime),
		GlobalFields: map[string]string{
			"name": "Global",
		},
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)

	template, err := q.GetTemplate(t.Context(), res.Msg.TemplateId)
	assert.NoError(t, err)
	assert.Equal(t, "Hello Global", template.Html)
}

func TestSendTemplateWithGlobalFields(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	schedTime := time.Now().Add(10 * time.Minute).Truncate(1 * time.Second)

	tmp, err := q.CreateTemplate(t.Context(), sqlc.CreateTemplateParams{
		Html:       "Hello {{ name }}",
		TemplateID: "test-template",
		Title:      "Test",
		Domain:     d.Domain.Domain,
		Type:       sqlc.TemplateTypeTemplate,
	})
	assert.NoError(t, err)

	req := connect.NewRequest(&mailerv1.SendTemplateReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Subject:       "Test",
		TemplateId:    tmp.TemplateID,
		ScheduledTime: timestamppb.New(schedTime),
		GlobalFields: map[string]string{
			"name": "Global",
		},
	})
	authRequest(req, d)

	res, err := ts.SendTemplate(t.Context(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)
	assert.NotEmpty(t, res.Msg.TemplateId)

	template, err := q.GetTemplate(t.Context(), res.Msg.TemplateId)
	assert.NoError(t, err)
	assert.NotEqual(t, tmp.ID, template.ID)
	assert.Equal(t, "Hello Global", template.Html)
}
