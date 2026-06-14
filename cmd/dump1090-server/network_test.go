package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

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

	svc := newNetworkService("test output")
	heartbeatInterval := 50 * time.Millisecond

	// Accept loop: register each accepted connection as a passive client.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			nc := newNetworkClient(conn)
			svc.Add(conn.RemoteAddr().String(), nc)
		}
	}()

	// Heartbeat loop: send heartbeats to idle clients using production code.
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		heartbeat := []byte("heartbeat\n")
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				svc.sendHeartbeats(heartbeat, heartbeatInterval)
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

	if got := svc.ClientCount(); got != 1 {
		t.Fatalf("expected 1 client, got %d", got)
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
	svc := newNetworkService("test output")

	// Create a connected pair: server-side and client-side.
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	nc := newNetworkClient(serverConn)
	svc.Add("test-client", nc)

	if got := svc.ClientCount(); got != 1 {
		t.Fatalf("expected 1 client before close, got %d", got)
	}

	// Close the client side so the next write on serverConn fails.
	clientConn.Close()

	// Broadcast after close should detect the error and remove via production code.
	svc.Broadcast([]byte("data\n"))

	if got := svc.ClientCount(); got != 0 {
		t.Fatalf("expected 0 clients after write failure, got %d", got)
	}
}

func TestDisabledPortDoesNotListen(t *testing.T) {
	// Verify production isPortDisabled rejects ports that must not start listeners.
	disabledCases := []struct {
		name string
		port int
	}{
		{"port 0", 0},
		{"negative port", -1},
	}
	for _, tc := range disabledCases {
		if !isPortDisabled(tc.port) {
			t.Errorf("isPortDisabled(%d) = false, want true (%s)", tc.port, tc.name)
		}
	}

	// Valid ports must not be flagged as disabled.
	enabledCases := []int{1, 8080, 30005, 65535}
	for _, port := range enabledCases {
		if isPortDisabled(port) {
			t.Errorf("isPortDisabled(%d) = true, want false", port)
		}
	}
}

func TestNetworkClientCloseIsIdempotent(t *testing.T) {
	// Close must be safe to call concurrently from Broadcast and
	// sendHeartbeats when both observe a write failure on the same client.
	server, client := net.Pipe()
	defer client.Close()

	nc := newNetworkClient(server)

	// Hammer Close from many goroutines simultaneously.
	var wg sync.WaitGroup
	const goroutines = 20
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = nc.Close()
		}(i)
	}
	wg.Wait()

	// Every call must return the same result (all nil, since first close wins).
	for i, err := range errs {
		if err != nil {
			t.Errorf("Close() goroutine %d returned error: %v", i, err)
		}
	}

	// A second round of closes after the first completed must also be safe.
	for i := 0; i < 5; i++ {
		if err := nc.Close(); err != nil {
			t.Errorf("post-close Close() call %d returned error: %v", i, err)
		}
	}
}

func TestCloseAllRemovesAllClients(t *testing.T) {
	svc := newNetworkService("test")

	s1, c1 := net.Pipe()
	defer c1.Close()
	s2, c2 := net.Pipe()
	defer c2.Close()

	svc.Add("a", newNetworkClient(s1))
	svc.Add("b", newNetworkClient(s2))

	if got := svc.ClientCount(); got != 2 {
		t.Fatalf("expected 2 clients, got %d", got)
	}

	svc.CloseAll()

	if got := svc.ClientCount(); got != 0 {
		t.Fatalf("expected 0 clients after CloseAll, got %d", got)
	}
}

func TestHeartbeatZeroIntervalDoesNotWriteOrRemove(t *testing.T) {
	// sendHeartbeats with interval <= 0 must be a no-op: no writes,
	// no removals, even when clients are idle.
	svc := newNetworkService("test")

	server, client := net.Pipe()
	defer client.Close()

	nc := newNetworkClient(server)
	svc.Add("idle-client", nc)

	// Call with zero interval.
	svc.sendHeartbeats([]byte("hb\n"), 0)
	// Call with negative interval.
	svc.sendHeartbeats([]byte("hb\n"), -5*time.Second)

	if got := svc.ClientCount(); got != 1 {
		t.Fatalf("expected 1 client after zero/negative heartbeat, got %d", got)
	}

	// Verify nothing was written to the client side.
	client.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	buf := make([]byte, 64)
	_, err := client.Read(buf)
	if err == nil {
		t.Fatal("expected read timeout (no data written), got data")
	}
}
