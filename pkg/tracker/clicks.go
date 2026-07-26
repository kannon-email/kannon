package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/trackingpb"
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	data := buildClickStat(claims, kept, domain)

	if err := publisher.PublishStat(s.pub, data); err != nil {
		slog.Error("cannot send message on nats", "err", err)
		return
	}

	slog.Info(fmt.Sprintf("🔗 %s %s %s %s %s %s", r.Method, claims.URL, claims.MessageID, kept.userAgent, r.Host, kept.ip))
}

func writeRedirect(w http.ResponseWriter, r *http.Request, claims *statssec.LinkClaims) {
	http.Redirect(w, r, claims.URL, http.StatusTemporaryRedirect)
}

func buildClickStat(claims *statssec.LinkClaims, kept engagement, domain string) *pb.Stats {
	data := &pb.Stats{
		MessageId: claims.MessageID,
		Email:     kept.email,
		Domain:    domain,
		Data: &pb.StatsData{
			Data: &pb.StatsData_Clicked{
				Clicked: &pb.StatsDataClicked{
					UserAgent: kept.userAgent,
					Ip:        kept.ip,
					Url:       claims.URL,
				},
			},
		},
		Type: string(sqlc.StatsTypeClicked),
		// The links Mode of the Delivery, for the same reason it travels on an
		// Opened: absent fields alone do not say why they are absent.
		TrackingMode: trackingpb.FromMode(claims.Mode),
		Timestamp:    timestamppb.Now(),
	}
	return data
}
