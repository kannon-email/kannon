package hzapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/pkg/hz"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	hzv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	"github.com/kannon-email/kannon/x/container"
)

type hzAPIConnectAdapter struct {
	hzService hz.HZService
}

func (h *hzAPIConnectAdapter) HZ(ctx context.Context, req *connect.Request[pb.HZRequest]) (*connect.Response[pb.HZResponse], error) {
	statuses := make(map[string]string)
	for name, err := range h.hzService.HZ(ctx) {
		statuses[name] = componentStatus(name, err)
	}
	return connect.NewResponse(&pb.HZResponse{Result: statuses}), nil
}

// StatusOK and StatusUnavailable are the only values a component's status takes on the wire.
const (
	StatusOK          = "ok"
	StatusUnavailable = "unavailable"
)

// componentStatus keeps a failure's detail in the log: the service answers unauthenticated
// callers, and a driver error names the database user, the database and the host.
func componentStatus(name string, err error) string {
	if err == nil {
		return StatusOK
	}
	slog.Warn("health check failed", "component", name, "err", err)
	return StatusUnavailable
}

func CreateHZAPIService(cnt *container.Container) hzv1connect.HZServiceHandler {
	hzService := hz.NewHZ(cnt)
	return &hzAPIConnectAdapter{
		hzService: hzService,
	}
}
