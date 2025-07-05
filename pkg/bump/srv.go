package bump

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/ludusrusso/kannon/internal/x/container"
	pb "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type srv struct {
	nc *nats.Conn
	ss statssec.StatsService
}

func NewServer(cnt *container.Container) *srv {
	nc := cnt.Nats()
	q := cnt.Queries()
	ss := statssec.NewStatsService(q)

	return &srv{
		nc: nc,
		ss: ss,
	}
}

func (s *srv) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/o/", s.handleOpen)
	mux.HandleFunc("/c/", s.handleClick)

	logrus.Infof("running bounce on %s", "localhost:8080")

	server := &http.Server{Addr: "0.0.0.0:8080", Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("error shutting down server: %v", err)
		}
	}()

	return server.ListenAndServe()
}

func (s *srv) handleClick(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	token := strings.Replace(r.URL.Path, "/c/", "", 1)
	claims, err := s.ss.VerifyLinkToken(ctx, token)
	if err != nil {
		logrus.Errorf("cannot verify click token: %v", err)
		http.NotFound(w, r)
		return
	}

	domain, err := utils.ExtractDomainFromMessageID(claims.MessageID)
	if err != nil {
		logrus.Errorf("cannot verify click token: %v", err)
		http.NotFound(w, r)
		return
	}

	defer writeRedirect(w, r, claims)

	userAgent := r.UserAgent()
	ip := readUserIP(r)
	data := buildClickStat(claims, userAgent, ip, domain)

	err = s.sendMsg(data, "kannon.stats.clicks")
	if err != nil {
		logrus.Errorf("cannot send message on nats: %v", err)
		return
	}

	logrus.Infof("🔗 %s %s %s %s %s %s", r.Method, claims.URL, claims.MessageID, r.Header["User-Agent"], r.Host, ip)
}

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

func (s *srv) sendMsg(data *pb.Stats, topic string) error {
	msg, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("cannot marshal data: %w", err)
	}
	err = s.nc.Publish(topic, msg)
	if err != nil {
		return fmt.Errorf("cannot send message on nats: %w", err)
	}
	return nil
}
