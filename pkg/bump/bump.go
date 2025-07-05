package bump

import (
	"context"
	"net/http"

	"github.com/ludusrusso/kannon/internal/x/container"
)

func Run(ctx context.Context, cnt *container.Container) error {
	srv := NewServer(cnt)
	return srv.Run(ctx)
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
