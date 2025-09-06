package hzapi

import (
	"context"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/pkg/hz"
	pb "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	hzv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
)

type hzAPIConnectAdapter struct {
	hzService hz.HZService
}

func (h *hzAPIConnectAdapter) HZ(ctx context.Context, req *connect.Request[pb.HZRequest]) (*connect.Response[pb.HZResponse], error) {
	result := h.hzService.HZ(ctx)

	// Convert map[string]error to map[string]string
	stringResult := make(map[string]string)
	for name, err := range result {
		if err != nil {
			stringResult[name] = err.Error()
		} else {
			stringResult[name] = "OK"
		}
	}

	response := &pb.HZResponse{
		Result: stringResult,
	}

	return connect.NewResponse(response), nil
}

func CreateHZAPIService(cnt *container.Container) hzv1connect.HZServiceHandler {
	hzService := hz.NewHZ(cnt)
	return &hzAPIConnectAdapter{
		hzService: hzService,
	}
}
