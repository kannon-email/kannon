package cmd

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartEmbeddedNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping embedded NATS test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	assert.True(t, ready, "NATS server should be ready within 5 seconds")

	// Verify server is running
	assert.True(t, ns.ReadyForConnections(100*time.Millisecond), "Server should remain ready")

	// Get client URL
	clientURL := ns.ClientURL()
	assert.NotEmpty(t, clientURL, "Client URL should not be empty")
	assert.Contains(t, clientURL, "127.0.0.1", "Client URL should use localhost")
}

func TestEmbeddedNATSConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping embedded NATS connection test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	require.True(t, ready, "NATS server should be ready within 5 seconds")

	// Connect to the embedded NATS server
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err, "Should be able to connect to embedded NATS")
	require.NotNil(t, nc, "NATS connection should not be nil")
	defer nc.Close()

	// Verify connection is established
	assert.True(t, nc.IsConnected(), "NATS client should be connected")
	assert.Equal(t, nats.CONNECTED, nc.Status(), "NATS client status should be CONNECTED")
}

func TestEmbeddedNATSJetStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping embedded NATS JetStream test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	require.True(t, ready, "NATS server should be ready within 5 seconds")

	// Connect to the embedded NATS server
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err, "Should be able to connect to embedded NATS")
	require.NotNil(t, nc, "NATS connection should not be nil")
	defer nc.Close()

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err, "Should be able to get JetStream context")
	require.NotNil(t, js, "JetStream context should not be nil")

	// Verify JetStream is available
	_, err = js.AccountInfo()
	assert.NoError(t, err, "Should be able to get JetStream account info")
}

func TestEmbeddedNATSPubSub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping embedded NATS pub/sub test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	require.True(t, ready, "NATS server should be ready within 5 seconds")

	// Connect to the embedded NATS server
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err, "Should be able to connect to embedded NATS")
	require.NotNil(t, nc, "NATS connection should not be nil")
	defer nc.Close()

	// Create a subscription
	subject := "test.subject"
	msgChan := make(chan *nats.Msg, 1)
	sub, err := nc.ChanSubscribe(subject, msgChan)
	require.NoError(t, err, "Should be able to subscribe")
	defer func() {
		err := sub.Unsubscribe()
		assert.NoError(t, err, "Should be able to unsubscribe")
	}()

	// Publish a message
	testMessage := []byte("test message")
	err = nc.Publish(subject, testMessage)
	require.NoError(t, err, "Should be able to publish message")

	// Wait for message with timeout
	select {
	case msg := <-msgChan:
		assert.Equal(t, testMessage, msg.Data, "Received message should match sent message")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestInitializeJetStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping JetStream initialization test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	require.True(t, ready, "NATS server should be ready within 5 seconds")

	// Connect to the embedded NATS server
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err, "Should be able to connect to embedded NATS")
	require.NotNil(t, nc, "NATS connection should not be nil")
	defer nc.Close()

	// Manually initialize JetStream streams
	js, err := nc.JetStream()
	require.NoError(t, err, "Should be able to get JetStream context")

	// Define the streams
	streams := []struct {
		name     string
		subjects []string
	}{
		{
			name:     "kannon_sending",
			subjects: []string{"kannon.sending"},
		},
		{
			name:     "kannon_stats",
			subjects: []string{"kannon.stats.*"},
		},
		{
			name:     "kannon_bounce",
			subjects: []string{"kannon.bounce"},
		},
	}

	// Create streams
	for _, stream := range streams {
		_, err := js.AddStream(&nats.StreamConfig{
			Name:     stream.name,
			Subjects: stream.subjects,
			Storage:  nats.MemoryStorage,
		})
		require.NoError(t, err, "Should be able to create stream %s", stream.name)
	}

	// Verify streams were created
	expectedStreams := []string{"kannon_sending", "kannon_stats", "kannon_bounce"}
	for _, streamName := range expectedStreams {
		info, err := js.StreamInfo(streamName)
		assert.NoError(t, err, "Stream %s should exist", streamName)
		assert.NotNil(t, info, "Stream info for %s should not be nil", streamName)
		assert.Equal(t, streamName, info.Config.Name, "Stream name should match")
	}
}

func TestInitializeJetStreamStreamsConfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping JetStream stream configuration test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATS()
	require.NoError(t, err, "Failed to start embedded NATS server")
	require.NotNil(t, ns, "NATS server should not be nil")
	defer ns.Shutdown()

	// Wait for server to be ready
	ready := ns.ReadyForConnections(5 * time.Second)
	require.True(t, ready, "NATS server should be ready within 5 seconds")

	// Connect to the embedded NATS server
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err, "Should be able to connect to embedded NATS")
	require.NotNil(t, nc, "NATS connection should not be nil")
	defer nc.Close()

	// Get JetStream context
	js, err := nc.JetStream()
	require.NoError(t, err, "Should be able to get JetStream context")

	testCases := []struct {
		name     string
		subjects []string
	}{
		{"kannon_sending", []string{"kannon.sending"}},
		{"kannon_stats", []string{"kannon.stats.*"}},
		{"kannon_bounce", []string{"kannon.bounce"}},
	}

	// Create and verify stream configurations
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create stream
			_, err := js.AddStream(&nats.StreamConfig{
				Name:     tc.name,
				Subjects: tc.subjects,
				Storage:  nats.MemoryStorage,
			})
			require.NoError(t, err, "Should be able to create stream %s", tc.name)

			// Verify stream configuration
			info, err := js.StreamInfo(tc.name)
			require.NoError(t, err, "Stream %s should exist", tc.name)
			assert.Equal(t, tc.subjects, info.Config.Subjects, "Stream subjects should match")
			assert.Equal(t, nats.MemoryStorage, info.Config.Storage, "Stream should use memory storage")
		})
	}
}
