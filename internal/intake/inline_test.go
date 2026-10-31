package intake

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/apperr"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/batch"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/require"
)

// memoryInlineStore keeps one receipt per Domain/key and the work that created
// it, the way the database adapter does inside one transaction.
type memoryInlineStore struct {
	InlineStore
	templates map[string]*templates.Template
	receipts  map[string]memoryReceipt
	works     []*InlineWork
	builds    int
}

type memoryReceipt struct {
	fingerprint string
	receipt     InlineReceipt
}

func (s *memoryInlineStore) FindByDomain(_ context.Context, _ values.DomainName, id string) (*templates.Template, error) {
	tpl, ok := s.templates[id]
	if !ok {
		return nil, apperr.New(apperr.NotFound, "template_not_found", "")
	}
	return tpl, nil
}

func (s *memoryInlineStore) CreateInline(_ context.Context, domain values.DomainName, key, fingerprint string, build func() (*InlineWork, error)) (*InlineReceipt, bool, error) {
	if s.receipts == nil {
		s.receipts = map[string]memoryReceipt{}
	}
	if kept, ok := s.receipts[domain.String()+":"+key]; ok {
		if kept.fingerprint != fingerprint {
			return nil, false, ErrIdempotencyConflict
		}
		return &kept.receipt, true, nil
	}
	s.builds++
	work, err := build()
	if err != nil {
		return nil, false, err
	}
	s.works = append(s.works, work)
	s.receipts[domain.String()+":"+key] = memoryReceipt{fingerprint: fingerprint, receipt: work.Receipt}
	return &work.Receipt, false, nil
}

type memoryDomains struct {
	domains.Repository
	domain *domains.Domain
}

func (r memoryDomains) FindByName(context.Context, values.DomainName) (*domains.Domain, error) {
	return r.domain, nil
}

var inlineDomain = values.MustParse("example.com")

func inlineRequest() InlineRequest {
	return InlineRequest{
		Sender:     batch.Sender{Email: "sender@example.com"},
		Subject:    "Hello {{ name }}",
		HTML:       "<p>Hello {{ name }}</p>",
		Recipients: []InlineRecipient{{Email: "alice@example.com", Fields: map[string]string{"name": "Alice"}}},
	}
}

func requireValidationFailed(t *testing.T, err error) {
	t.Helper()
	refusal, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, apperr.Invalid, refusal.Kind)
	require.Equal(t, "validation_failed", refusal.Code)
}

func TestValidateInline(t *testing.T) {
	many := make([]InlineRecipient, maxInlineRecipients+1)
	for _, tc := range []struct {
		name   string
		key    string
		change func(*InlineRequest)
		want   error // a sentinel
		bad    bool  // or a validation failure
	}{
		{name: "valid", key: "k"},
		{name: "valid template", key: "k", change: func(p *InlineRequest) { p.HTML, p.TemplateID = "", "tpl" }},
		{name: "empty key", key: "", bad: true},
		{name: "key too long", key: strings.Repeat("k", 257), bad: true},
		{name: "key with padding", key: " k", bad: true},
		{name: "key with newline", key: "k\nk", bad: true},
		// Too many Recipients is reported before any content fault.
		{name: "too many recipients", key: "k", change: func(p *InlineRequest) { p.Recipients, p.Subject = many, "" }, want: ErrTooManyRecipients},
		{name: "no subject", key: "k", change: func(p *InlineRequest) { p.Subject = "" }, bad: true},
		{name: "subject with newline", key: "k", change: func(p *InlineRequest) { p.Subject = "a\r\nb" }, bad: true},
		{name: "alias with newline", key: "k", change: func(p *InlineRequest) { p.Sender.Alias = "a\nb" }, bad: true},
		{name: "no recipients", key: "k", change: func(p *InlineRequest) { p.Recipients = nil }, bad: true},
		{name: "html and template", key: "k", change: func(p *InlineRequest) { p.TemplateID = "tpl" }, bad: true},
		{name: "neither html nor template", key: "k", change: func(p *InlineRequest) { p.HTML = "" }, bad: true},
		{name: "unparsable display header", key: "k", change: func(p *InlineRequest) { p.Headers.To = []string{"not an address"} }, bad: true},
		{name: "display header with newline", key: "k", change: func(p *InlineRequest) { p.Headers.Cc = []string{"a@example.com\n"} }, bad: true},
		{name: "reserved global field", key: "k", change: func(p *InlineRequest) { p.GlobalFields = map[string]string{"email": "x"} }, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := inlineRequest()
			if tc.change != nil {
				tc.change(&p)
			}
			err := validateInline(tc.key, p)
			switch {
			case tc.want != nil:
				require.ErrorIs(t, err, tc.want)
			case tc.bad:
				requireValidationFailed(t, err)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestInlineSchedule(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d).In(time.FixedZone("x", 3600)); return &v }
	got, err := inlineSchedule(nil, now)
	require.NoError(t, err)
	require.Equal(t, now, got)
	for _, past := range []*time.Time{at(0), at(-time.Second)} {
		_, err := inlineSchedule(past, now)
		require.ErrorIs(t, err, ErrScheduledAtNotFuture)
	}
	// Sub-microsecond precision is dropped before comparison, as persistence would.
	_, err = inlineSchedule(at(time.Nanosecond), now)
	require.ErrorIs(t, err, ErrScheduledAtNotFuture)
	got, err = inlineSchedule(at(time.Hour+time.Nanosecond), now)
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Hour), got)
	require.Equal(t, time.UTC, got.Location())
}

func TestInlineAttachments(t *testing.T) {
	attachments, metadata, err := inlineAttachments([]Attachment{{Filename: "b.txt", Content: []byte("bb")}, {Filename: "a.txt", Content: []byte("a")}})
	require.NoError(t, err)
	require.Equal(t, batch.Attachments{"a.txt": []byte("a"), "b.txt": []byte("bb")}, attachments)
	require.Equal(t, []AttachmentMetadata{{Filename: "b.txt", SizeBytes: 2}, {Filename: "a.txt", SizeBytes: 1}}, metadata)

	_, metadata, err = inlineAttachments(nil)
	require.NoError(t, err)
	require.Equal(t, []AttachmentMetadata{}, metadata)

	_, _, err = inlineAttachments([]Attachment{{Filename: "a.txt"}, {Filename: "a.txt"}})
	require.ErrorIs(t, err, ErrDuplicateAttachmentFilename)
	for _, name := range []string{"", "a\r\n.txt"} {
		_, _, err = inlineAttachments([]Attachment{{Filename: name}})
		requireValidationFailed(t, err)
	}
}

func TestDecideRecipients(t *testing.T) {
	anonymous := tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous}
	full := tracking.Policy{Opens: tracking.ModeFull}
	unsupported := tracking.Policy{Links: tracking.Mode("telepathic")}
	for _, tc := range []struct {
		name       string
		ceiling    tracking.Policy
		change     func(*InlineRequest)
		recipients []InlineRecipient
		want       []RejectionReason // "" accepts
	}{
		{name: "accepted", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}}}, want: []RejectionReason{""}},
		{name: "invalid email", recipients: []InlineRecipient{{Email: "not-an-address"}, {Email: " "}}, want: []RejectionReason{ReasonInvalidEmail, ReasonInvalidEmail}},
		{
			name:       "duplicate after domain normalization",
			recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}}, {Email: " a@EXAMPLE.com ", Fields: map[string]string{"name": "A"}}, {Email: "A@example.com", Fields: map[string]string{"name": "A"}}},
			want:       []RejectionReason{"", ReasonDuplicateRecipient, ""},
		},
		{name: "refused address still counts as seen", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"email": "x", "name": "A"}}, {Email: "a@example.com", Fields: map[string]string{"name": "A"}}}, want: []RejectionReason{ReasonReservedField, ReasonDuplicateRecipient}},
		{name: "reserved field", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"email": "x", "name": "A"}}}, want: []RejectionReason{ReasonReservedField}},
		{name: "unresolved placeholder in body", recipients: []InlineRecipient{{Email: "a@example.com"}}, want: []RejectionReason{ReasonUnresolvedPlaceholder}},
		{name: "unresolved placeholder in subject", change: func(p *InlineRequest) { p.HTML = "<p>Hi</p>" }, recipients: []InlineRecipient{{Email: "a@example.com"}}, want: []RejectionReason{ReasonUnresolvedPlaceholder}},
		{name: "global fields resolve placeholders", change: func(p *InlineRequest) { p.GlobalFields = map[string]string{"name": "All"} }, recipients: []InlineRecipient{{Email: "a@example.com"}}, want: []RejectionReason{""}},
		// Values are substituted once and never expanded, so one that looks like a
		// placeholder is left in the output and refuses the Recipient.
		{name: "values are not templates", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "{{ other }}", "other": "x"}}}, want: []RejectionReason{ReasonUnresolvedPlaceholder}},
		{name: "subject newline from field", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A\r\nBcc: x"}}}, want: []RejectionReason{ReasonReservedField}},
		{name: "unsupported tracking mode", recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}, Tracking: unsupported}}, want: []RejectionReason{ReasonUnsupportedTrackingMode}},
		{name: "tracking above domain ceiling", ceiling: anonymous, recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}, Tracking: full}}, want: []RejectionReason{ReasonTrackingAboveCeiling}},
		{name: "tracking above batch policy", change: func(p *InlineRequest) { p.Tracking = anonymous }, recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}, Tracking: full}}, want: []RejectionReason{ReasonTrackingAboveCeiling}},
		{
			name: "unsubscribe url unresolved",
			change: func(p *InlineRequest) {
				p.OneClickUnsubscribe = batch.OneClickUnsubscribe{URLTemplate: "https://example.com/leave?t={{ token }}"}
			},
			recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}}, {Email: "b@example.com", Fields: map[string]string{"name": "B", "token": "t"}}},
			want:       []RejectionReason{ReasonUnsubscribeURLUnresolved, ""},
		},
		// The first failing check names the refusal.
		{name: "invalid email outranks reserved field", recipients: []InlineRecipient{{Email: "nope", Fields: map[string]string{"email": "x"}}}, want: []RejectionReason{ReasonInvalidEmail}},
		{name: "placeholder outranks tracking", ceiling: anonymous, recipients: []InlineRecipient{{Email: "a@example.com", Tracking: full}}, want: []RejectionReason{ReasonUnresolvedPlaceholder}},
		{name: "unsupported outranks ceiling", ceiling: anonymous, recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}, Tracking: tracking.Policy{Opens: tracking.ModeFull, Links: "telepathic"}}}, want: []RejectionReason{ReasonUnsupportedTrackingMode}},
		{
			name:    "ceiling outranks unsubscribe",
			ceiling: anonymous,
			change: func(p *InlineRequest) {
				p.OneClickUnsubscribe = batch.OneClickUnsubscribe{URLTemplate: "https://example.com/leave?t={{ token }}"}
			},
			recipients: []InlineRecipient{{Email: "a@example.com", Fields: map[string]string{"name": "A"}, Tracking: full}},
			want:       []RejectionReason{ReasonTrackingAboveCeiling},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := inlineRequest()
			p.Recipients = tc.recipients
			if tc.change != nil {
				tc.change(&p)
			}
			accepted, refusals := decideRecipients(tc.ceiling, p.HTML, p)
			var wantRefusals []InlineRefusal
			wantAccepted := 0
			for i, reason := range tc.want {
				if reason == "" {
					wantAccepted++
					continue
				}
				wantRefusals = append(wantRefusals, InlineRefusal{Index: i, Code: string(reason)})
			}
			require.Equal(t, wantRefusals, refusals)
			require.Len(t, accepted, wantAccepted)
		})
	}
}

func TestDecideRecipientsFreezesFieldsAndTracking(t *testing.T) {
	p := inlineRequest()
	p.GlobalFields = map[string]string{"name": "All", "team": "Ops"}
	p.Tracking = tracking.Policy{Opens: tracking.ModeIdentified}
	p.Recipients = []InlineRecipient{{Email: " Alice@EXAMPLE.com", Fields: map[string]string{"name": "Alice"}, Tracking: tracking.Policy{Opens: tracking.ModeAnonymous}}}
	accepted, refusals := decideRecipients(tracking.Policy{Opens: tracking.ModeFull, Links: tracking.ModeAnonymous}, p.HTML, p)
	require.Empty(t, refusals)
	require.Equal(t, []inlineRecipient{{
		email:    "Alice@example.com",
		fields:   map[string]string{"name": "Alice", "team": "Ops", "email": "Alice@example.com"},
		tracking: tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeAnonymous},
	}}, accepted)
}

func TestInlineCreate(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ceiling := tracking.Policy{Opens: tracking.ModeAnonymous}
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(inlineDomain))))
	service := func() (*InlineService, *memoryInlineStore) {
		source, err := templates.NewPersistent(inlineDomain, "<p>Dear {{ name }}</p>", "Source")
		require.NoError(t, err)
		store := &memoryInlineStore{templates: map[string]*templates.Template{"tpl": source}}
		s := NewInlineService(store, memoryDomains{domain: domains.Load(domains.LoadParams{Domain: inlineDomain, Tracking: ceiling})}, nil, 0, 0)
		s.Now = func() time.Time { return now.Add(500 * time.Nanosecond) }
		return s, store
	}

	t.Run("immediate send is pending at receipt time", func(t *testing.T) {
		s, store := service()
		p := inlineRequest()
		p.Attachments = []Attachment{{Filename: "a.txt", Content: []byte("abc")}}
		p.Recipients = append(p.Recipients, InlineRecipient{Email: "invalid"}, InlineRecipient{Email: "bob@example.com", Fields: map[string]string{"name": "Bob"}})
		receipt, refusals, replay, err := s.Create(ctx, inlineDomain, "key", p)
		require.NoError(t, err)
		require.False(t, replay)
		require.Equal(t, []InlineRefusal{{Index: 1, Code: "invalid_email"}}, refusals)
		require.Len(t, store.works, 1)
		work := store.works[0]
		require.Equal(t, InlineReceipt{ID: work.Batch.ID().String(), Domain: "example.com", CreatedAt: now, ScheduledAt: now, Attachments: []AttachmentMetadata{{Filename: "a.txt", SizeBytes: 3}}, AcceptedCount: 2, RejectedCount: 1}, *receipt)
		require.Equal(t, p.HTML, work.Template.Html())
		require.Equal(t, work.Template.TemplateID(), work.Batch.TemplateID())
		require.Empty(t, work.SourceTemplateID)
		require.Len(t, work.Deliveries, 2)
		for _, d := range work.Deliveries {
			require.Equal(t, receipt.CreatedAt, d.ScheduledTime())
			require.Equal(t, work.Batch.ID(), d.BatchID())
			require.Equal(t, tracking.Policy{Opens: tracking.ModeAnonymous, Links: tracking.ModeOff}, d.TrackingPolicy())
		}
		require.Equal(t, "alice@example.com", work.Deliveries[0].Email())
		require.Equal(t, "bob@example.com", work.Deliveries[1].Email())
	})

	t.Run("replay returns the receipt without building", func(t *testing.T) {
		s, store := service()
		p := inlineRequest()
		p.Recipients = append(p.Recipients, InlineRecipient{Email: "invalid"})
		first, _, _, err := s.Create(ctx, inlineDomain, "key", p)
		require.NoError(t, err)
		// A rule whose answer changed since creation does not refuse the replay.
		s.Now = func() time.Time { return now.Add(time.Hour) }
		again, refusals, replay, err := s.Create(ctx, inlineDomain, "key", p)
		require.NoError(t, err)
		require.True(t, replay)
		require.Nil(t, refusals)
		require.Equal(t, first, again)
		require.Equal(t, 1, store.builds)

		p.Subject = "Changed {{ name }}"
		_, _, _, err = s.Create(ctx, inlineDomain, "key", p)
		require.ErrorIs(t, err, ErrIdempotencyConflict)
		require.Equal(t, 1, store.builds)
	})

	t.Run("template source is snapshotted", func(t *testing.T) {
		s, store := service()
		p := inlineRequest()
		p.HTML, p.TemplateID = "", "tpl"
		_, _, _, err := s.Create(ctx, inlineDomain, "key", p)
		require.NoError(t, err)
		require.Equal(t, "<p>Dear {{ name }}</p>", store.works[0].Template.Html())
		require.Equal(t, "tpl", store.works[0].SourceTemplateID)
		require.NotEqual(t, "tpl", store.works[0].Template.TemplateID())
	})

	t.Run("refused requests persist nothing", func(t *testing.T) {
		later := now.Add(time.Hour)
		for _, tc := range []struct {
			name     string
			key      string
			change   func(*InlineRequest)
			want     error  // a sentinel
			code     string // or the public code of a wrapped refusal
			refusals []InlineRefusal
			built    bool
		}{
			{name: "invalid key", key: " key", change: func(*InlineRequest) {}, code: "validation_failed"},
			{name: "too many recipients", change: func(p *InlineRequest) { p.Recipients = make([]InlineRecipient, maxInlineRecipients+1) }, want: ErrTooManyRecipients},
			{name: "batch tracking above ceiling", change: func(p *InlineRequest) { p.Tracking = tracking.Policy{Opens: tracking.ModeFull} }, code: "validation_failed", built: true},
			{name: "unsupported batch tracking", change: func(p *InlineRequest) { p.Tracking = tracking.Policy{Links: "telepathic"} }, code: "validation_failed", built: true},
			{name: "schedule not in future", change: func(p *InlineRequest) { p.ScheduledAt = &now }, want: ErrScheduledAtNotFuture, built: true},
			{name: "duplicate attachment", change: func(p *InlineRequest) { p.Attachments = []Attachment{{Filename: "a"}, {Filename: "a"}} }, want: ErrDuplicateAttachmentFilename, built: true},
			{name: "missing template", change: func(p *InlineRequest) { p.HTML, p.TemplateID = "", "missing" }, code: "template_not_found", built: true},
			{
				name: "no recipient accepted",
				change: func(p *InlineRequest) {
					p.ScheduledAt = &later
					p.Recipients = []InlineRecipient{{Email: "x"}, {Email: "a@example.com"}}
				},
				want:     ErrNoRecipientsAccepted,
				refusals: []InlineRefusal{{Index: 0, Code: "invalid_email"}, {Index: 1, Code: "unresolved_placeholder"}},
				built:    true,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, store := service()
				p := inlineRequest()
				tc.change(&p)
				key := "key"
				if tc.key != "" {
					key = tc.key
				}
				receipt, refusals, replay, err := s.Create(ctx, inlineDomain, key, p)
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
				} else {
					got, ok := apperr.As(err)
					require.True(t, ok, "%v", err)
					require.Equal(t, tc.code, got.Code)
				}
				require.Nil(t, receipt)
				require.False(t, replay)
				require.Equal(t, tc.refusals, refusals)
				require.Empty(t, store.works)
				require.Empty(t, store.receipts)
				require.Equal(t, tc.built, store.builds == 1)
			})
		}
	})

	t.Run("unrelated sender is refused before storage", func(t *testing.T) {
		s, store := service()
		p := inlineRequest()
		p.Sender.Email = "sender@other.example"
		_, _, _, err := s.Create(ctx, inlineDomain, "key", p)
		require.ErrorIs(t, err, authz.ErrForbidden)
		require.Zero(t, store.builds)
	})
}

// The cap is 70% of the largest Envelope the transport carries: with 1000, 700 bytes of HTML
// plus base64 attachments. A referenced Template's HTML counts like inline HTML.
func TestInlineCreateRefusesMessageTooLarge(t *testing.T) {
	ctx := authz.NewContext(t.Context(), authz.MustNewPrincipal("key", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(inlineDomain))))
	source, err := templates.NewPersistent(inlineDomain, "<p>Dear {{ name }}</p>", "Source")
	require.NoError(t, err)
	store := &memoryInlineStore{templates: map[string]*templates.Template{"tpl": source}}
	s := NewInlineService(store, memoryDomains{domain: domains.Load(domains.LoadParams{Domain: inlineDomain})}, nil, 0, 1000)

	// 507 raw bytes encode to 676; with the 23-byte inline body that is 699.
	p := inlineRequest()
	p.Attachments = []Attachment{{Filename: "a.pdf", Content: make([]byte, 507)}}
	receipt, _, _, err := s.Create(ctx, inlineDomain, "fits", p)
	require.NoError(t, err)
	require.Equal(t, 1, receipt.AcceptedCount)

	// 510 raw bytes encode to 680, which fits alone; the Template's 22 bytes make it 702.
	p = inlineRequest()
	p.HTML, p.TemplateID = "", "tpl"
	p.Attachments = []Attachment{{Filename: "a.pdf", Content: make([]byte, 510)}}
	receipt, _, _, err = s.Create(ctx, inlineDomain, "too-large", p)
	require.ErrorIs(t, err, ErrMessageTooLarge)
	require.Nil(t, receipt)
	require.Len(t, store.works, 1)
}
