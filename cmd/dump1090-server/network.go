package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// NetworkService manages a set of output clients for a single TCP service.
// Output clients are passive readers: the server never reads from them.
// Clients are removed only on write failure or context cancellation.
type NetworkService struct {
	name    string
	clients sync.Map
	nextID  atomic.Uint64
}

// newNetworkService creates a new NetworkService.
func newNetworkService(name string) *NetworkService {
	return &NetworkService{name: name}
}

// newClientID returns a collision-proof client ID combining the remote
// address with a monotonic counter (e.g. "127.0.0.1:54321#1").
func (svc *NetworkService) newClientID(remoteAddr string) string {
	id := svc.nextID.Add(1)
	return fmt.Sprintf("%s#%d", remoteAddr, id)
}

// Add registers a client connection.
func (svc *NetworkService) Add(id string, nc *NetworkClient) {
	svc.clients.Store(id, nc)
}

// CloseAll closes and unregisters every client. Call on server shutdown.
// Safe to call concurrently with Broadcast and sendHeartbeats because
// NetworkClient.Close (sync.Once) and sync.Map.Delete are both idempotent.
func (svc *NetworkService) CloseAll() {
	svc.clients.Range(func(key, value any) bool {
		value.(*NetworkClient).Close()
		svc.clients.Delete(key)
		return true
	})
}

// ClientCount returns the number of registered clients.
func (svc *NetworkService) ClientCount() int {
	n := 0
	svc.clients.Range(func(_, _ any) bool { n++; return true })
	return n
}

// Broadcast writes data to all registered clients. Clients that fail
// to receive the data are removed and closed.
func (svc *NetworkService) Broadcast(data []byte) {
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

// sendHeartbeats sends a heartbeat to clients that have been idle
// for longer than the given interval. A zero or negative interval
// disables heartbeats (no writes, no removals).
func (svc *NetworkService) sendHeartbeats(heartbeat []byte, interval time.Duration) {
	if interval <= 0 {
		return
	}
	svc.clients.Range(func(key, value any) bool {
		client := value.(*NetworkClient)
		if client.shouldSendHeartbeat(interval) {
			_, err := client.Write(heartbeat)
			if err != nil {
				client.Close()
				svc.clients.Delete(key)
			}
		}
		return true
	})
}

// isPortDisabled reports whether a port value indicates the service
// should not start a listener. Port 0 or negative means disabled.
func isPortDisabled(port int) bool {
	return port <= 0
}

// runOutputTCPServer listens for connections and registers them as passive
// output clients. Unlike input servers, it does not read from clients;
// connections are kept until context cancellation or write failure.
func (app *App) runOutputTCPServer(svc *NetworkService, port int) {
	defer app.wg.Done()
	defer svc.CloseAll()

	if isPortDisabled(port) {
		return
	}

	addr := fmt.Sprintf("%s:%d", app.config.NetBindAddress, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("%s server error: %v", svc.name, err)
		return
	}
	defer listener.Close()

	log.Printf("%s server listening on %s", svc.name, addr)

	go func() {
		<-app.ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-app.ctx.Done():
				return
			default:
				log.Printf("%s accept error: %v", svc.name, err)
				continue
			}
		}

		clientID := svc.newClientID(conn.RemoteAddr().String())
		client := newNetworkClient(conn)
		svc.Add(clientID, client)
	}
}
