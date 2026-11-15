package container

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Listen binds a TCP listener on every interface. Port 0 lets the system pick a free one.
func Listen(port uint) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
}

// ServeHTTP serves on a listener bound beforehand, and waits for active requests before returning
// ownership of dependencies.
func ServeHTTP(ctx context.Context, server *http.Server, l net.Listener) error {
	done := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := server.Shutdown(shutdownCtx)
			if err != nil {
				_ = server.Close()
			}
			stopped <- err
		case <-done:
			stopped <- nil
		}
	}()
	err := server.Serve(l)
	close(done)
	shutdownErr := <-stopped
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
