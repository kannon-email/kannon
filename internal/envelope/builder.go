package envelope

import (
	"bytes"
	"context"
	"fmt"

	"github.com/kannon-email/kannon/internal/batch"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/dkim"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/utils"
)

const maxRetry = 10

// SendingData is the per-Batch lookup the Builder needs to render an
// outgoing Envelope: template HTML + Domain DKIM keys + Batch metadata.
// It is populated by a SendingDataSource at the storage boundary.
type SendingData struct {
	Subject        string
	HTML           string
	Domain         string
	MessageID      string
	SenderEmail    string
	SenderAlias    string
	DkimPrivateKey string
	Attachments    map[string][]byte
	Headers        batch.Headers
}

// SendingDataSource looks up the rendering inputs for a Batch.
type SendingDataSource interface {
	GetSendingData(ctx context.Context, batchID batch.ID) (SendingData, error)
}

// TokenIssuer mints click/open tokens for tracking link rewriting. Each token
// carries the Tracking Mode of the axis it belongs to — the opens Mode on an
// open token, the links Mode on a link token — signed, so the Tracker can trust
// it without a Delivery row to look it up in.
//
// A Mode that does not identify the Recipient mints no identity into the token,
// so the Builder asks for such a token once per Batch and reuses it for every
// Delivery — see sharedtokens.go.
type TokenIssuer interface {
	CreateLinkToken(ctx context.Context, messageID, email, url string, mode tracking.Mode) (string, error)
	CreateOpenToken(ctx context.Context, messageID, email string, mode tracking.Mode) (string, error)
}

// Builder renders a Delivery into an outgoing Envelope.
type Builder interface {
	Build(ctx context.Context, d *delivery.Delivery) (*Envelope, error)
}

// NewBuilder returns the default Builder backed by sqlc and the given
// stats service. The sqlc-backed source resolves the Batch + Template +
// Domain join in a single query (see internal/db/pool.sql).
func NewBuilder(q *sqlc.Queries, st statssec.StatsService) Builder {
	return &defaultBuilder{
		source: sqlcSource{q: q},
		tokens: st,
		shared: newSharedTokens(),
		baseHeaders: headers{
			"X-Mailer": {"SMTP Mailer"},
		},
	}
}

// NewBuilderWith wires a Builder against an explicit source + token issuer.
// Useful for unit tests that want to stub both sides.
func NewBuilderWith(source SendingDataSource, tokens TokenIssuer) Builder {
	return &defaultBuilder{
		source: source,
		tokens: tokens,
		shared: newSharedTokens(),
		baseHeaders: headers{
			"X-Mailer": {"SMTP Mailer"},
		},
	}
}

type defaultBuilder struct {
	source SendingDataSource
	tokens TokenIssuer
	// shared holds the tokens that name no Recipient, so they are issued once per
	// Batch rather than once per Delivery. It is per-Builder, and a Builder lives
	// as long as the Dispatcher does.
	shared      *sharedTokens
	baseHeaders headers
}

func (b *defaultBuilder) Build(ctx context.Context, d *delivery.Delivery) (*Envelope, error) {
	data, err := b.source.GetSendingData(ctx, d.BatchID())
	if err != nil {
		return nil, err
	}

	attachments := make(Attachments)
	for name, raw := range data.Attachments {
		attachments[name] = bytes.NewReader(raw)
	}

	returnPath := buildReturnPath(d.Email(), data.MessageID)
	msg, err := b.prepareMessage(ctx, d, data, attachments)
	if err != nil {
		return nil, err
	}

	hasCc := len(data.Headers.Cc) > 0
	signedMsg, err := signMessage(data.Domain, data.DkimPrivateKey, msg, hasCc)
	if err != nil {
		return nil, err
	}

	return New(Params{
		EmailID:     buildEmailID(d.Email(), data.MessageID),
		From:        data.SenderEmail,
		To:          d.Email(),
		ReturnPath:  returnPath,
		Body:        signedMsg,
		ShouldRetry: d.SendAttempts() < maxRetry,
	}), nil
}

func (b *defaultBuilder) prepareMessage(ctx context.Context, d *delivery.Delivery, data SendingData, attachments Attachments) ([]byte, error) {
	emailMessageID := buildEmailID(d.Email(), data.MessageID)
	html, err := b.preparedHTML(ctx, d, data)
	if err != nil {
		return nil, err
	}
	subject := utils.ReplaceCustomFields(data.Subject, d.Fields())

	sender := batch.Sender{Email: data.SenderEmail, Alias: data.SenderAlias}
	h := buildHeaders(subject, sender, d.Email(), data.MessageID, emailMessageID, b.baseHeaders, data.Headers)
	return renderMsg(html, h, attachments)
}

func signMessage(domain, dkimPrivateKey string, msg []byte, hasCc bool) ([]byte, error) {
	dkimHeaders := []string{"From", "To", "Subject", "Message-ID"}
	if hasCc {
		dkimHeaders = append(dkimHeaders, "Cc")
	}

	signData := dkim.SignData{
		PrivateKey: dkimPrivateKey,
		Domain:     domain,
		Selector:   "kannon",
		Headers:    dkimHeaders,
	}

	return dkim.SignMessage(signData, bytes.NewReader(msg))
}

// preparedHTML renders the Batch template for one Delivery and applies the
// Delivery's frozen Tracking Policy. The cascade was already resolved at intake
// (ADR 0003), so the Builder reads the Policy as it stands: it never resolves it
// again and never consults configuration.
//
// The two axes are independent, and each Off suppresses only its own channel: no
// pixel is injected for opens, no href is rewritten for links. A Mode that
// states nothing imposes no restriction, so every Mode other than Off is tracked
// as before.
//
// Whatever the Mode of a tracked axis is, it is minted into the token of that
// axis, so the Tracker acts on the Policy frozen on this Delivery rather than on
// whatever is configured when the engagement arrives.
func (b *defaultBuilder) preparedHTML(ctx context.Context, d *delivery.Delivery, data SendingData) (string, error) {
	policy := d.TrackingPolicy()
	html := utils.ReplaceCustomFields(data.HTML, d.Fields())

	if policy.Links != tracking.ModeOff {
		rewritten, err := b.replaceAllLinks(ctx, html, trackTargetFor(d, data, policy.Links))
		if err != nil {
			return "", err
		}
		html = rewritten
	}

	// A link that opted out of tracking has now been left as authored — but the
	// attribute that asked for it is addressed to Kannon, not to the recipient, so
	// it is dropped here rather than inside the rewriting step. That is what keeps
	// it out of the delivered HTML under a links Mode of Off too, where nothing was
	// rewritten and no link was even looked at.
	html = stripNoTrackAttrs(html)

	if policy.Opens == tracking.ModeOff {
		return html, nil
	}
	return b.addTrackPixel(ctx, html, trackTargetFor(d, data, policy.Opens))
}

// trackTarget is what one channel's tracking URL is minted against: which
// Recipient, in which Batch and Domain, under which Mode. The four travel
// together through every step of rewriting, and the Mode is the one that decides
// whether the resulting token may be shared across the Batch.
type trackTarget struct {
	email     string
	messageID string
	domain    string
	mode      tracking.Mode
}

// trackTargetFor is the target for one axis of a Delivery, taking the Mode of
// that axis from the Policy frozen on it — so the Tracker acts on the Policy that
// governed this Delivery rather than on whatever is configured when the
// engagement arrives.
func trackTargetFor(d *delivery.Delivery, data SendingData, mode tracking.Mode) trackTarget {
	return trackTarget{
		email:     d.Email(),
		messageID: data.MessageID,
		domain:    data.Domain,
		mode:      mode,
	}
}

func (b *defaultBuilder) replaceAllLinks(ctx context.Context, html string, t trackTarget) (string, error) {
	return replaceLinks(html, func(link string) (string, error) {
		return b.buildTrackClickLink(ctx, link, t)
	})
}

func (b *defaultBuilder) addTrackPixel(ctx context.Context, html string, t trackTarget) (string, error) {
	link, err := b.buildTrackOpenLink(ctx, t)
	if err != nil {
		return "", err
	}
	return insertTrackLinkInHTML(html, link), nil
}

func (b *defaultBuilder) buildTrackClickLink(ctx context.Context, url string, t trackTarget) (string, error) {
	token, err := b.token(sharedTokenKey{
		axis:      tracking.AxisLinks,
		domain:    t.domain,
		messageID: t.messageID,
		url:       url,
		mode:      t.mode,
	}, func() (string, error) {
		return b.tokens.CreateLinkToken(ctx, t.messageID, t.email, url, t.mode)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://stats.%v/c/%v", t.domain, token), nil
}

func (b *defaultBuilder) buildTrackOpenLink(ctx context.Context, t trackTarget) (string, error) {
	token, err := b.token(sharedTokenKey{
		axis:      tracking.AxisOpens,
		domain:    t.domain,
		messageID: t.messageID,
		mode:      t.mode,
	}, func() (string, error) {
		return b.tokens.CreateOpenToken(ctx, t.messageID, t.email, t.mode)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://stats.%v/o/%v", t.domain, token), nil
}

// token returns the token for one channel of one Delivery. Under a Mode that
// names the Recipient it is minted per Delivery, because that is what it commits
// to. Under a Mode that names nobody it commits only to what the key holds, so
// every Delivery of the Batch carries the very same token — the privacy property
// of such a Mode, and the reason one signature covers a whole Batch.
//
// The rule lives here once, for both channels, so that the key and the decision
// to share cannot drift apart: a key missing anything the token commits to would
// hand one Recipient's token to another.
func (b *defaultBuilder) token(key sharedTokenKey, mint func() (string, error)) (string, error) {
	if key.mode.IdentifiesRecipient() {
		return mint()
	}
	return b.shared.reuse(key, mint)
}

// sqlcSource adapts the sqlc-generated GetSendingData query into the
// domain-friendly SendingData type the Builder consumes.
type sqlcSource struct {
	q *sqlc.Queries
}

func (s sqlcSource) GetSendingData(ctx context.Context, batchID batch.ID) (SendingData, error) {
	row, err := s.q.GetSendingData(ctx, batchID.String())
	if err != nil {
		return SendingData{}, err
	}

	atts := make(map[string][]byte, len(row.Attachments))
	for name, raw := range row.Attachments {
		atts[name] = raw
	}

	return SendingData{
		Subject:        row.Subject,
		HTML:           row.Html,
		Domain:         row.Domain,
		MessageID:      row.MessageID,
		SenderEmail:    row.SenderEmail,
		SenderAlias:    row.SenderAlias,
		DkimPrivateKey: row.DkimPrivateKey,
		Attachments:    atts,
		Headers: batch.Headers{
			To: row.Headers.To,
			Cc: row.Headers.Cc,
		},
	}, nil
}
