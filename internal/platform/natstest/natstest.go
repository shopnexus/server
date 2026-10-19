// Package natstest runs a NATS server with JetStream inside the test process,
// one per test, so streams and consumers never leak between tests.
package natstest

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Start runs a server for t and returns a connection and a JetStream handle.
// Both are shut down when the test ends.
func Start(t testing.TB) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	s, err := start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return nc, js
}

// StartForMain runs a server for TestMain and returns its URL and a stop
// function.
func StartForMain() (string, func(), error) {
	dir, err := os.MkdirTemp("", "natstest")
	if err != nil {
		return "", nil, err
	}
	s, err := start(dir)
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return s.ClientURL(), func() { s.Shutdown(); s.WaitForShutdown(); os.RemoveAll(dir) }, nil
}

func start(storeDir string) (*server.Server, error) {
	s, err := server.NewServer(&server.Options{
		Host:      "127.0.0.1",
		Port:      server.RANDOM_PORT,
		JetStream: true,
		StoreDir:  storeDir,
		NoLog:     true,
		NoSigs:    true,
	})
	if err != nil {
		return nil, err
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		return nil, fmt.Errorf("natstest: server not ready")
	}
	return s, nil
}
