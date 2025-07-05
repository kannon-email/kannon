package bump

import (
	"image"
	"net/http"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/statssec"
	pb "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
