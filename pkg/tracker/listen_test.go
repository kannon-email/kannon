package tracker

import (
	"net"
	"testing"

	"github.com/spf13/viper"
)

func TestListenOnPortZeroPicksAFreePort(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set(PortKey, 0)

	l, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	if port := l.Addr().(*net.TCPAddr).Port; port == 0 { //nolint:errcheck // a TCP listener has a TCP address
		t.Fatal("expected the system to pick a port")
	}
}
