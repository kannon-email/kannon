package container

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/kannon-email/kannon/x/config"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)
	return addr.Port
}

func newNatsServer(t *testing.T, port int) *server.Server {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port})
	require.NoError(t, err)
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	return ns
}

func TestNatsWaitsForAServerThatComesUpLate(t *testing.T) {
	port := freePort(t)
	ns := newNatsServer(t, port)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	c := New(ctx, config.RootConfig{NatsURL: fmt.Sprintf("nats://127.0.0.1:%d", port)})
	t.Cleanup(func() { assert.NoError(t, c.CloseWithTimeout(5*time.Second)) })

	go func() {
		time.Sleep(1500 * time.Millisecond)
		ns.Start()
	}()

	nc, err := c.TryNats(ctx)

	require.NoError(t, err)
	assert.Equal(t, nats.CONNECTED, nc.Status())
}

func TestNatsGivesUpWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	c := New(t.Context(), config.RootConfig{NatsURL: fmt.Sprintf("nats://127.0.0.1:%d", freePort(t))})

	_, err := c.TryNats(ctx)

	assert.Error(t, err)
}
