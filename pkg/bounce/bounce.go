package bounce

import (
	"context"
	"errors"
	"image"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ludusrusso/kannon/generated/pb"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var retImage = image.NewGray(image.Rect(0, 0, 0, 0))

func Run(ctx context.Context, vc *viper.Viper) {
	vc.SetEnvPrefix("BOUNCE")
	dbURL := vc.GetString("database_url")
	natsURL := vc.GetString("nats_url")
	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	nc, js, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()
	mustConfigureOpenJS(js)
	mustConfigureClickJS(js)

	ss := statssec.NewStatsService(q)

	http.HandleFunc("/o/", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			w.Header().Set("Content-Type", "image/png")
			w.Write(retImage.Pix)
		}()

		ctx, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		token := strings.Replace(r.URL.Path, "/o/", "", 1)
		claims, err := ss.VertifyOpenToken(ctx, token)
		if err != nil {
			logrus.Errorf("cannot verify open token: %v", err)
			return
		}
		ip := readUserIP(r)
		data := &pb.Open{
			MessageId: claims.MessageID,
			Email:     claims.Email,
			Ip:        ip,
			UserAgent: r.UserAgent(),
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

	logrus.Infof("running bounce on %s", "localhost:8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
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

func mustConfigureOpenJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-stats-opens",
		Description: "Email Open for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.opens"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}

func mustConfigureClickJS(js nats.JetStreamContext) {
	confs := nats.StreamConfig{
		Name:        "kannon-stats-clicks",
		Description: "Email Click for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.clicks"},
		Retention:   nats.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
	}
	info, err := js.AddStream(&confs)
	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		logrus.Infof("stream exists")
	} else if err != nil {
		logrus.Fatalf("cannot create js stream: %v", err)
	}
	logrus.Infof("created js stream: %v", info)
}
