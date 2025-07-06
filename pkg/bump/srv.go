package bump

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/x/container"
	pb "github.com/kannon-email/kannon/proto/kannon/stats/types"
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
