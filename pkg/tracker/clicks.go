package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
)

func (s *srv) handleClick(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	token := strings.Replace(r.URL.Path, "/c/", "", 1)
	claims, err := s.ss.VerifyLinkToken(ctx, token)
	if err != nil {
		slog.Error("cannot verify click token", "err", err)
		http.NotFound(w, r)
		return
	}

	domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
	if err != nil {
		slog.Error("cannot verify click token", "err", err)
		http.NotFound(w, r)
		return
	}

	defer writeRedirect(w, r, claims)

	// As for opens: the Mode is whatever the signed claims say. Anonymous names
	// nobody, and only Full retains anything about the request itself.
	kept := retained(r, claims.Email, claims.Mode)
	event := buildClickEvent(claims, kept, domain)

	if err := publisher.PublishStat(s.pub, event); err != nil {
		slog.Error("cannot send message on nats", "err", err)
		return
	}

	slog.Info(fmt.Sprintf("🔗 %s %s %s %s %s %s", r.Method, claims.URL, claims.MessageID, kept.userAgent, r.Host, kept.ip))
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
