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

	userAgent := r.UserAgent()
	ip := readUserIP(r)
	data := buildClickStat(claims, userAgent, ip, domain)

	if err := publisher.PublishStat(s.pub, data); err != nil {
		slog.Error("cannot send message on nats", "err", err)
		return
	}

	slog.Info(fmt.Sprintf("🔗 %s %s %s %s %s %s", r.Method, claims.URL, claims.MessageID, r.Header["User-Agent"], r.Host, ip))
}

func writeRedirect(w http.ResponseWriter, r *http.Request, claims *statssec.LinkClaims) {
	http.Redirect(w, r, claims.URL, http.StatusTemporaryRedirect)
}

func buildClickStat(claims *statssec.LinkClaims, userAgent string, ip string, domain string) *pb.Stats {
	data := &pb.Stats{
		MessageId: claims.MessageID,
		Email:     claims.Email,
		Domain:    domain,
		Data: &pb.StatsData{
			Data: &pb.StatsData_Clicked{
				Clicked: &pb.StatsDataClicked{
					UserAgent: userAgent,
					Ip:        ip,
					Url:       claims.URL,
				},
			},
		},
		Type:      string(sqlc.StatsTypeClicked),
		Timestamp: timestamppb.Now(),
	}
	return data
}
