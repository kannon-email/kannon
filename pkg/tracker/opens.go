package tracker

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
)

func (s *srv) handleOpen(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	token := strings.Replace(r.URL.Path, "/o/", "", 1)
	claims, err := s.ss.VerifyOpenToken(ctx, token)
	if err != nil {
		slog.Error(fmt.Sprintf("cannot verify open token: %v", err))
		http.NotFound(w, r)
		return
	}

	domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
	if err != nil {
		slog.Error(fmt.Sprintf("cannot verify open token: %v", err))
		http.NotFound(w, r)
		return
	}

	defer writeTrackingPixel(w)

	// The Mode comes from the verified claims: the Delivery row it was frozen on
	// may be long gone, and reading it from the request would let a recipient
	// choose how much is retained about them.
	kept := retained(r, claims.Email, claims.Mode)
	event := buildOpenEvent(claims, kept, domain)

	if err := publisher.PublishStat(s.pub, event); err != nil {
		slog.Error("cannot send message on nats", "err", err)
		return
	}

	slog.Info(fmt.Sprintf("👀 %s %s %s %s %s", r.Method, claims.MessageID, kept.userAgent, r.Host, kept.ip))
}

var trackingPixel = image.NewGray(image.Rect(0, 0, 0, 0))

func writeTrackingPixel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(trackingPixel.Pix); err != nil {
		slog.Error("cannot write image", "err", err)
	}
}

func buildOpenEvent(claims *statssec.OpenClaims, kept engagement, domain string) stats.Event {
	return stats.Event{
		MessageID: claims.MessageID,
		Email:     kept.email,
		Domain:    domain,
		Outcome:   stats.Opened(kept.userAgent, kept.ip),
		// The Mode travels on the event so a consumer can tell an Opened with no
		// ip / user_agent because Identified forbade retaining them from one that
		// merely lacks them — and, under Anonymous, an event with no email from a
		// bug that lost one.
		TrackingMode: claims.Mode,
		Timestamp:    time.Now(),
	}
}
