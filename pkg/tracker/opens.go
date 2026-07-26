package tracker

import (
	"context"
	"fmt"
	"image"
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
	ip, userAgent := retained(r, claims.Mode)
	data := buildOpenStat(claims, userAgent, ip, domain)

	if err := publisher.PublishStat(s.pub, data); err != nil {
		slog.Error("cannot send message on nats", "err", err)
		return
	}

	slog.Info(fmt.Sprintf("👀 %s %s %s %s %s", r.Method, claims.MessageID, userAgent, r.Host, ip))
}

var trackingPixel = image.NewGray(image.Rect(0, 0, 0, 0))

func writeTrackingPixel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(trackingPixel.Pix); err != nil {
		slog.Error("cannot write image", "err", err)
	}
}

func buildOpenStat(claims *statssec.OpenClaims, userAgent string, ip string, domain string) *pb.Stats {
	data := &pb.Stats{
		MessageId: claims.MessageID,
		Email:     claims.Email,
		Data: &pb.StatsData{
			Data: &pb.StatsData_Opened{
				Opened: &pb.StatsDataOpened{
					UserAgent: userAgent,
					Ip:        ip,
				},
			},
		},
		Domain: domain,
		Type:   string(sqlc.StatsTypeOpened),
		// The Mode travels on the event so a consumer can tell an Opened with no
		// ip / user_agent because Identified forbade retaining them from one that
		// merely lacks them.
		TrackingMode: trackingpb.FromMode(claims.Mode),
		Timestamp:    timestamppb.Now(),
	}
	return data
}
