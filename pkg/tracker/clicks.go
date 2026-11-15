package tracker

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/transient"
	"github.com/kannon-email/kannon/internal/utils"
)

func (s *srv) handleClick(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	token := strings.Replace(r.URL.Path, "/c/", "", 1)
	claims, err := s.ss.VerifyLinkToken(ctx, token)
	if err != nil {
		slog.Warn("rejecting click: token does not verify", "component", "tracker", "host", r.Host, "err", err)
		http.NotFound(w, r)
		return
	}

	domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
	if err != nil {
		slog.Warn("rejecting click: token names no domain", "component", "tracker", "host", r.Host, "err", err)
		http.NotFound(w, r)
		return
	}
	if !servesDomain(r, domain) {
		slog.Warn("rejecting click: token presented on another Domain's host", "component", "tracker", "host", r.Host, "domain", domain)
		http.NotFound(w, r)
		return
	}
	if !isWebURL(claims.URL) {
		slog.Warn("rejecting click: token names a non-web URL", "component", "tracker", "domain", domain)
		http.NotFound(w, r)
		return
	}

	defer writeRedirect(w, r, claims)

	// As for opens: the Mode is whatever the signed claims say. Anonymous names
	// nobody, and only Full retains anything about the request itself.
	kept := retained(r, claims.Email, claims.Mode)
	event := buildClickEvent(claims, kept, domain)

	if err := publisher.PublishStat(s.pub, event); err != nil {
		slog.Log(ctx, transient.Level(err), "cannot publish click stat", "component", "tracker", "batch_id", claims.MessageID, "err", err)
		return
	}
	s.flow.Engaged(mailflow.Clicked)

	// The link's host and not the whole URL: a query string can carry the recipient's own tokens.
	slog.Info("click tracked", append(kept.logAttrs(), "component", "tracker", "batch_id", claims.MessageID, "host", r.Host, "link_host", linkHost(claims.URL))...)
}

func isWebURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

func writeRedirect(w http.ResponseWriter, r *http.Request, claims *statssec.LinkClaims) {
	http.Redirect(w, r, claims.URL, http.StatusTemporaryRedirect)
}

func buildClickEvent(claims *statssec.LinkClaims, kept engagement, domain string) stats.Event {
	return stats.Event{
		MessageID: claims.MessageID,
		Email:     kept.email,
		Domain:    domain,
		Outcome:   stats.Clicked(kept.userAgent, kept.ip, claims.URL),
		// The links Mode of the Delivery, for the same reason it travels on an
		// Opened: absent fields alone do not say why they are absent.
		TrackingMode: claims.Mode,
		Timestamp:    time.Now(),
	}
}
