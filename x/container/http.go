package container

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ServeHTTP waits for active requests before returning ownership of dependencies.
func ServeHTTP(ctx context.Context, server *http.Server) error {
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
	err := server.ListenAndServe()
	close(done)
	shutdownErr := <-stopped
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
