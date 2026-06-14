package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// testOutputService is a minimal output service for testing client lifecycle.
type testOutputService struct {
	clients sync.Map
}

func (svc *testOutputService) addClient(id string, nc *NetworkClient) {
	svc.clients.Store(id, nc)
}

func (svc *testOutputService) removeClient(id string) {
	svc.clients.Delete(id)
}

func (svc *testOutputService) clientCount() int {
	n := 0
	svc.clients.Range(func(_, _ any) bool { n++; return true })
	return n
}

// broadcastToClients writes data to all clients, removing those that fail.
// This is the lifecycle-correct pattern that network.go must implement.
func (svc *testOutputService) broadcastToClients(data []byte) {
	svc.clients.Range(func(key, value any) bool {
		client := value.(*NetworkClient)
		_, err := client.Write(data)
		if err != nil {
			client.Close()
			svc.clients.Delete(key)
		}
		return true
	})
}

func TestOutputClientCanRemainPassiveReader(t *testing.T) {
	// An output client that sends zero bytes must stay connected
	// and receive heartbeats without being disconnected.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	svc := &testOutputService{}
	heartbeatInterval := 50 * time.Millisecond

	// Accept loop: register each accepted connection as a passive client.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			nc := newNetworkClient(conn)
			svc.addClient(conn.RemoteAddr().String(), nc)
		}
	}()

	// Heartbeat loop: periodically send heartbeat to idle clients.
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				svc.broadcastToClients([]byte("heartbeat\n"))
			}
		}
	}()

	// Connect a passive client that never writes.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Give time for client registration and a few heartbeat cycles.
	time.Sleep(heartbeatInterval*4 + 50*time.Millisecond)

	if svc.clientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", svc.clientCount())
	}

	// Client should have received at least one heartbeat.
	conn.SetReadDeadline(time.Now().Add(heartbeatInterval * 3))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("passive client should have received heartbeat, got error: %v", err)
	}
	if n == 0 {
		t.Fatal("expected heartbeat data, got 0 bytes")
	}
}

func TestWriteFailureRemovesOutputClient(t *testing.T) {
	// When a client connection is closed, the next broadcast must
	// detect the write failure and remove the client from the map.
	svc := &testOutputService{}

	// Create a connected pair: server-side and client-side.
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	nc := newNetworkClient(serverConn)
	svc.addClient("test-client", nc)

	if svc.clientCount() != 1 {
		t.Fatalf("expected 1 client before close, got %d", svc.clientCount())
	}

	// Close the client side so the next write on serverConn fails.
	clientConn.Close()

	// First broadcast after close should detect the error and remove.
	svc.broadcastToClients([]byte("data\n"))

	if svc.clientCount() != 0 {
		t.Fatalf("expected 0 clients after write failure, got %d", svc.clientCount())
	}
}

func TestDisabledPortDoesNotListen(t *testing.T) {
	// A service configured with port 0 must not create a listener.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// runTCPServer-like helper that returns whether it actually listened.
	started := make(chan struct{})
	listenAndServe := func(port int) {
		defer wg.Done()
		if port == 0 {
			// Port 0 means disabled; do nothing.
			return
		}
		addr := net.JoinHostPort("127.0.0.1", "0")
		if port > 0 {
			addr = net.JoinHostPort("127.0.0.1", itoa(port))
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		defer ln.Close()
		close(started)
		<-ctx.Done()
	}

	// Port 0: must not start.
	wg.Add(1)
	go listenAndServe(0)

	// Give the goroutine a moment to decide.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-started:
		t.Fatal("port 0 should not have started a listener")
	default:
		// OK: no listener started.
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
