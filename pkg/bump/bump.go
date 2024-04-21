package bump

import (
	"context"
	"image"
	"net/http"
	"strings"
	"time"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/utils"
	pb "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var retImage = image.NewGray(image.Rect(0, 0, 0, 0))

func Run(ctx context.Context) {
	dbURL := viper.GetString("database_url")
	natsURL := viper.GetString("nats_url")

	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	nc, _, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()

	ss := statssec.NewStatsService(q)

	http.HandleFunc("/o/", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			w.Header().Set("Content-Type", "image/png")
			if _, err := w.Write(retImage.Pix); err != nil {
				logrus.Errorf("cannot write image: %v", err)
			}
		}()

		ctx, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		token := strings.Replace(r.URL.Path, "/o/", "", 1)
		claims, err := ss.VertifyOpenToken(ctx, token)
		if err != nil {
			logrus.Errorf("cannot verify open token: %v", err)
			return
		}
		domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
		if err != nil {
			logrus.Errorf("cannot verify open token: %v", err)
			http.NotFound(w, r)
			return
		}

		ip := readUserIP(r)
		data := &pb.Stats{
			MessageId: claims.MessageID,
			Email:     claims.Email,
			Data: &pb.StatsData{
				Data: &pb.StatsData_Opened{
					Opened: &pb.StatsDataOpened{
						UserAgent: r.UserAgent(),
						Ip:        ip,
					},
				},
			},
			Domain:    domain,
			Type:      string(sqlc.StatsTypeOpened),
			Timestamp: timestamppb.Now(),
		}
		msg, err := proto.Marshal(data)
		if err != nil {
			logrus.Errorf("Cannot marshal data: %v", err)
			return
		}
		err = nc.Publish("kannon.stats.opens", msg)
		if err != nil {
			logrus.Errorf("Cannot send message on nats: %v", err)
			return
		}
		logrus.Infof("👀 %s %s %s %s %s", r.Method, claims.MessageID, r.Header["User-Agent"], r.Host, ip)
	})

	http.HandleFunc("/c/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		token := strings.Replace(r.URL.Path, "/c/", "", 1)
		claims, err := ss.VertifyLinkToken(ctx, token)
		if err != nil {
			logrus.Errorf("cannot verify open token: %v", err)
			http.NotFound(w, r)
			return
		}

		defer func() {
			http.Redirect(w, r, claims.URL, http.StatusTemporaryRedirect)
		}()

		domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
		if err != nil {
			logrus.Errorf("cannot verify open token: %v", err)
			http.NotFound(w, r)
			return
		}

		ip := readUserIP(r)
		data := &pb.Stats{
			MessageId: claims.MessageID,
			Email:     claims.Email,
			Domain:    domain,
			Data: &pb.StatsData{
				Data: &pb.StatsData_Clicked{
					Clicked: &pb.StatsDataClicked{
						UserAgent: r.UserAgent(),
						Ip:        ip,
						Url:       claims.URL,
					},
				},
			},
			Type:      string(sqlc.StatsTypeClicked),
			Timestamp: timestamppb.Now(),
		}
		msg, err := proto.Marshal(data)
		if err != nil {
			logrus.Errorf("Cannot marshal data: %v", err)
			return
		}
		err = nc.Publish("kannon.stats.clicks", msg)
		if err != nil {
			logrus.Errorf("Cannot send message on nats: %v", err)
			return
		}
		logrus.Infof("🔗 %s %s %s %s %s %s", r.Method, claims.URL, claims.MessageID, r.Header["User-Agent"], r.Host, ip)
	})

	logrus.Infof("running bounce on %s", "localhost:8080")

	if err := http.ListenAndServe("0.0.0.0:8080", nil); err != nil {
		logrus.Fatal(err)
	}
}

func readUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}
