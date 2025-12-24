package server

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestTCPListener_HelloExchange(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	server := New(&Spec{Storage: store})

	// Start TCP listener on random port
	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	addr := server.TCPAddr()
	if addr == "" {
		t.Fatal("expected TCP address")
	}
	t.Logf("TCP listener on %s", addr)

	// Connect client
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send hello request
	_, err = conn.Write([]byte(`{hello: {clientId: "test-client"}}` + "\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := bytes.TrimSpace(buf[:n])
	t.Logf("Response: %s", response)

	// Parse response
	var resp api.SessionResponse
	if err := resp.FromTony(response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Result == nil || resp.Result.Hello == nil {
		t.Fatal("expected hello response")
	}
	if resp.Result.Hello.ServerID == "" {
		t.Error("expected serverID to be set")
	}
}

func TestTCPListener_MatchRequest(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	server := New(&Spec{Storage: store})

	// Start TCP listener
	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	// Connect client
	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send match request
	_, err = conn.Write([]byte(`{id: "req-1", match: {body: {path: ""}}}` + "\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := bytes.TrimSpace(buf[:n])
	t.Logf("Response: %s", response)

	// Parse response
	var resp api.SessionResponse
	if err := resp.FromTony(response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.ID == nil || *resp.ID != "req-1" {
		t.Errorf("expected id 'req-1', got %v", resp.ID)
	}
	if resp.Result == nil || resp.Result.Match == nil {
		t.Fatal("expected match result")
	}
}

func TestTCPListener_MultipleClients(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	server := New(&Spec{Storage: store})

	// Start TCP listener
	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	addr := server.TCPAddr()

	// Connect multiple clients
	const numClients = 3
	conns := make([]net.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		conns[i] = conn
		defer conn.Close()
	}

	// Each client sends hello
	for i, conn := range conns {
		_, err := conn.Write([]byte(`{hello: {clientId: "client-` + string(rune('a'+i)) + `"}}` + "\n"))
		if err != nil {
			t.Fatalf("client %d failed to write: %v", i, err)
		}
	}

	// Each client reads response
	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("client %d failed to read: %v", i, err)
		}

		var resp api.SessionResponse
		if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
			t.Fatalf("client %d failed to parse response: %v", i, err)
		}

		if resp.Result == nil || resp.Result.Hello == nil {
			t.Errorf("client %d: expected hello response", i)
		}
	}

	// Give time for sessions to register
	time.Sleep(50 * time.Millisecond)

	// Check session count (TCP listener tracks this)
	if server.tcpListener.SessionCount() != numClients {
		t.Errorf("expected %d sessions, got %d", numClients, server.tcpListener.SessionCount())
	}

	// Close all connections
	for _, conn := range conns {
		conn.Close()
	}

	// Wait for sessions to end
	time.Sleep(100 * time.Millisecond)

	if server.tcpListener.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after close, got %d", server.tcpListener.SessionCount())
	}
}
