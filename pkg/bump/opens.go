package bump

import (
	"context"
	"image"
	"net/http"
	"strings"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/utils"
	pb "github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *srv) handleOpen(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	token := strings.Replace(r.URL.Path, "/o/", "", 1)
	claims, err := s.ss.VerifyOpenToken(ctx, token)
	if err != nil {
		logrus.Errorf("cannot verify open token: %v", err)
		http.NotFound(w, r)
		return
	}

	domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
	if err != nil {
		logrus.Errorf("cannot verify open token: %v", err)
		http.NotFound(w, r)
		return
	}

	defer writeTrackingPixel(w)

	userAgent := r.UserAgent()
	ip := readUserIP(r)
	data := buildOpenStat(claims, userAgent, ip, domain)

	err = s.sendMsg(data, "kannon.stats.opens")
	if err != nil {
		logrus.Errorf("cannot send message on nats: %v", err)
		return
	}

	logrus.Infof("👀 %s %s %s %s %s", r.Method, claims.MessageID, r.Header["User-Agent"], r.Host, ip)
}

var trackingPixel = image.NewGray(image.Rect(0, 0, 0, 0))

func writeTrackingPixel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(trackingPixel.Pix); err != nil {
		logrus.Errorf("cannot write image: %v", err)
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
		Domain:    domain,
		Type:      string(sqlc.StatsTypeOpened),
		Timestamp: timestamppb.Now(),
	}
	return data
}
