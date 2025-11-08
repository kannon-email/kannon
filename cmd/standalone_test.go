package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startEmbeddedNATSForTest starts an embedded NATS server for testing purposes
func startEmbeddedNATSForTest() (*server.Server, error) {
	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1, // Random available port
		JetStream: true,
		StoreDir:  "", // Use temp directory for storage
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS server: %w", err)
	}

	// Start the server in a goroutine
	go ns.Start()

	// Wait for server to be ready
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		return nil, fmt.Errorf("NATS server not ready after 10 seconds")
	}

	return ns, nil
}

func TestStartEmbeddedNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping embedded NATS test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATSForTest()
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
	ns, err := startEmbeddedNATSForTest()
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
	ns, err := startEmbeddedNATSForTest()
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
	ns, err := startEmbeddedNATSForTest()
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
	ns, err := startEmbeddedNATSForTest()
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

	// Call the actual helper function to initialize streams
	err = initializeJetStreamStreams(js)
	require.NoError(t, err, "Should be able to initialize JetStream streams")

	// Verify streams were created with correct configuration
	expectedStreams := []struct {
		name     string
		subjects []string
		storage  nats.StorageType
	}{
		{
			name:     "kannon-sending",
			subjects: []string{"kannon.sending"},
			storage:  nats.FileStorage,
		},
		{
			name:     "kannon-stats",
			subjects: []string{"kannon.stats.*"},
			storage:  nats.FileStorage,
		},
		{
			name:     "kannon-bounce",
			subjects: []string{"kannon.bounce"},
			storage:  nats.FileStorage,
		},
	}

	for _, expected := range expectedStreams {
		info, err := js.StreamInfo(expected.name)
		require.NoError(t, err, "Stream %s should exist", expected.name)
		require.NotNil(t, info, "Stream info for %s should not be nil", expected.name)
		assert.Equal(t, expected.name, info.Config.Name, "Stream name should match")
		assert.Equal(t, expected.subjects, info.Config.Subjects, "Stream subjects should match for %s", expected.name)
		assert.Equal(t, expected.storage, info.Config.Storage, "Stream storage should be FileStorage for %s", expected.name)
	}
}

func TestInitializeJetStreamStreamsConfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping JetStream stream configuration test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATSForTest()
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

	// Call the actual helper function to initialize streams
	err = initializeJetStreamStreams(js)
	require.NoError(t, err, "Should be able to initialize JetStream streams")

	testCases := []struct {
		name     string
		subjects []string
	}{
		{"kannon-sending", []string{"kannon.sending"}},
		{"kannon-stats", []string{"kannon.stats.*"}},
		{"kannon-bounce", []string{"kannon.bounce"}},
	}

	// Verify stream configurations
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify stream configuration
			info, err := js.StreamInfo(tc.name)
			require.NoError(t, err, "Stream %s should exist", tc.name)
			assert.Equal(t, tc.subjects, info.Config.Subjects, "Stream subjects should match")
			assert.Equal(t, nats.FileStorage, info.Config.Storage, "Stream should use file storage")
		})
	}
}

func TestInitializeJetStreamIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping JetStream idempotency test in short mode")
	}

	// Start embedded NATS server
	ns, err := startEmbeddedNATSForTest()
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

	// Call the helper function the first time
	err = initializeJetStreamStreams(js)
	require.NoError(t, err, "First call should succeed")

	// Call the helper function again - should be idempotent
	err = initializeJetStreamStreams(js)
	require.NoError(t, err, "Second call should succeed (idempotent)")

	// Verify streams still exist and are correctly configured
	expectedStreams := []string{"kannon-sending", "kannon-stats", "kannon-bounce"}
	for _, streamName := range expectedStreams {
		info, err := js.StreamInfo(streamName)
		require.NoError(t, err, "Stream %s should exist after idempotent calls", streamName)
		require.NotNil(t, info, "Stream info for %s should not be nil", streamName)
	}
}
