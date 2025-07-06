package bump

import (
	"context"

	"github.com/kannon-email/kannon/internal/x/container"
)

func Run(ctx context.Context, cnt *container.Container) error {
	srv := NewServer(cnt)
	return srv.Run(ctx)
}
