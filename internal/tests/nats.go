package tests

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NatsJetStream starts an in-process NATS server with JetStream enabled and
// returns a JetStream context connected to it. Server, connection and store
// directory are torn down when the test ends.
//
// This is the same embedded server the binary itself can run
// (x/container.EmbeddedNatsServer), so consumer and stream behaviour — ack
// deadlines, redelivery, key/value buckets — is the real thing rather than a
// stub, without the Docker round trip the e2e suite pays for.
func NatsJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()

	nc, err := nats.Connect(NatsURL(t))
	if err != nil {
		t.Fatalf("cannot connect to embedded NATS server: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("cannot create JetStream context: %v", err)
	}

	return js
}

// NatsURL starts an in-process NATS server with JetStream enabled and returns
// the URL to reach it. Server and store directory are torn down when the test
// ends.
func NatsURL(t *testing.T) string {
	t.Helper()

	ns, err := server.NewServer(&server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random available port
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("cannot create embedded NATS server: %v", err)
	}

	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		t.Fatal("embedded NATS server not ready")
	}

	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})

	return ns.ClientURL()
}
