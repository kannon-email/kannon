package mailapi_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/stretchr/testify/assert"
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
				_, err := adminAPI.SetTrackingPolicy(t.Context(), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
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

// TestSendMailBatchTrackingPolicy covers the Batch level of the Tracking
// Policy cascade (#417): a Batch may state a Policy at or below its Domain's
// ceiling, which is resolved onto every Delivery and kept verbatim on the
// Batch row as provenance; a Batch stating more than the ceiling allows fails
// the call outright rather than being silently clamped, naming the offending
// axis (ADR 0003); the two axes are evaluated independently; and a Batch
// stating the reserved `pseudonymous` Mode is rejected as unimplemented.
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
		_, err := adminAPI.SetTrackingPolicy(t.Context(), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
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
		_, err := adminAPI.SetTrackingPolicy(t.Context(), connect.NewRequest(&adminv1.SetTrackingPolicyReq{
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

	t.Run("PseudonymousIsRejectedAsUnimplemented", func(t *testing.T) {
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
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_PSEUDONYMOUS,
			},
		})
		authRequest(req, d)

		_, err := ts.SendHTML(t.Context(), req)
		assert.NotNil(t, err)
		assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
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

func TestSendMailSkipsRecipientsWithEmptyEmail(t *testing.T) {
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

	sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: res.Msg.MessageId,
		Limit:     100,
		Offset:    0,
	})
	assert.Nil(t, err)
	assert.Equal(t, 3, len(sp))

	got := []string{sp[0].Email, sp[1].Email, sp[2].Email}
	assert.ElementsMatch(t, []string{"first@email.com", "second@email.com", "third@email.com"}, got)
}

func TestSendMailWithAllEmptyRecipientsSucceedsWithEmptyPool(t *testing.T) {
	defer cleanDB(t)

	d := createTestDomain(t)

	req := connect.NewRequest(&mailerv1.SendHTMLReq{
		Sender: &types.Sender{
			Email: "test@" + d.Domain.Domain,
			Alias: "Test",
		},
		Recipients: []*types.Recipient{
			{Email: ""},
			{Email: "\t"},
		},
		Subject:       "Test",
		Html:          "Hello",
		ScheduledTime: timestamppb.Now(),
	})
	authRequest(req, d)

	res, err := ts.SendHTML(t.Context(), req)

	assert.Nil(t, err)
	assert.NotEmpty(t, res.Msg.MessageId)

	sp, err := q.GetSendingPoolsEmails(t.Context(), sqlc.GetSendingPoolsEmailsParams{
		MessageID: res.Msg.MessageId,
		Limit:     100,
		Offset:    0,
	})
	assert.Nil(t, err)
	assert.Equal(t, 0, len(sp))
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
