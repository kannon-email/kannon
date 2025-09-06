package hz

import (
	"context"

	"github.com/kannon-email/kannon/internal/x/container"
)

type HZService interface {
	HZ(context.Context) HZRes
}

type hZService struct {
	cnt *container.Container
}

func NewHZ(cnt *container.Container) *hZService {
	return &hZService{
		cnt: cnt,
	}
}

type HZRes = container.HZRes

func (h *hZService) HZ(ctx context.Context) HZRes {
	return h.cnt.HZ(ctx)
}
