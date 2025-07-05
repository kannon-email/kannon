package bump

import (
	"net/http"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/statssec"
	pb "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
