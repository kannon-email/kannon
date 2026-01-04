package bump

import (
	"context"

	"github.com/kannon-email/kannon/internal/x/container"
)

type Config struct {
	Port uint
}

func Run(ctx context.Context, cnt *container.Container, cfg Config) error {
	srv := NewServer(cnt, cfg)
	return srv.Run(ctx)
}
