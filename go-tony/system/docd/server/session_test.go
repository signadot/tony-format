package server

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/docd/api"
)

func TestMountHandshake_Success(t *testing.T) {
	server := New(&Spec{})

	// Start TCP listener on random port
	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	addr := server.TCPAddr()
	t.Logf("TCP listener on %s", addr)

	// Connect as controller
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send mount request
	mountReq := `{hello: {controller: "user-ctrl"}, mount: {path: "/users"}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
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
	var resp api.MountResponse
	if err := resp.FromTony(response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify success
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
	if resp.Result.Hello == nil {
		t.Fatal("expected hello in result")
	}
	if resp.Result.Hello.DocdID == "" {
		t.Error("expected docdId to be set")
	}
	if resp.Result.Mount == nil {
		t.Fatal("expected mount in result")
	}
	if resp.Result.Mount.Path != "/users" {
		t.Errorf("expected path '/users', got %q", resp.Result.Mount.Path)
	}
	if !resp.Result.Mount.Accepted {
		t.Error("expected accepted to be true")
	}

	// Verify mount is registered
	entry := server.Mounts.Lookup("/users")
	if entry == nil {
		t.Fatal("expected mount to be registered")
	}
	if entry.Controller != "user-ctrl" {
		t.Errorf("expected controller 'user-ctrl', got %q", entry.Controller)
	}
}

func TestMountHandshake_WithSchema(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send mount request with schema
	mountReq := `{hello: {controller: "user-ctrl"}, mount: {path: "/users", schema: {define: {User: {id: .string, name: .string}}, accept: {users: .array(.User)}}}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// Verify schema is stored
	entry := server.Mounts.Lookup("/users")
	if entry == nil {
		t.Fatal("expected mount to be registered")
	}
	if entry.Schema == nil {
		t.Error("expected schema to be stored")
	}
}

func TestMountHandshake_PathAlreadyMounted(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	addr := server.TCPAddr()

	// First controller mounts /users
	conn1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn1.Close()

	mountReq := `{hello: {controller: "ctrl-1"}, mount: {path: "/users"}}` + "\n"
	if _, err := conn1.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn1.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn1.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp1 api.MountResponse
	if err := resp1.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp1.Error != nil {
		t.Fatalf("first mount should succeed: %s", resp1.Error.Message)
	}

	// Second controller tries to mount same path
	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn2.Close()

	mountReq2 := `{hello: {controller: "ctrl-2"}, mount: {path: "/users"}}` + "\n"
	if _, err := conn2.Write([]byte(mountReq2)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn2.SetReadDeadline(time.Now().Add(time.Second))
	n, err = conn2.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp2 api.MountResponse
	if err := resp2.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should get error
	if resp2.Error == nil {
		t.Fatal("expected error for duplicate mount")
	}
	if resp2.Error.Code != api.ErrCodePathAlreadyMounted {
		t.Errorf("expected error code %q, got %q", api.ErrCodePathAlreadyMounted, resp2.Error.Code)
	}
	t.Logf("Got expected error: %s", resp2.Error.Message)
}

func TestMountHandshake_InvalidPath(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Path without leading slash
	mountReq := `{hello: {controller: "ctrl"}, mount: {path: "users"}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for invalid path")
	}
	if resp.Error.Code != api.ErrCodeInvalidPath {
		t.Errorf("expected error code %q, got %q", api.ErrCodeInvalidPath, resp.Error.Code)
	}
}

func TestMountHandshake_MissingHello(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Missing hello
	mountReq := `{mount: {path: "/users"}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for missing hello")
	}
	if resp.Error.Code != api.ErrCodeInvalidMessage {
		t.Errorf("expected error code %q, got %q", api.ErrCodeInvalidMessage, resp.Error.Code)
	}
}

func TestMountHandshake_MissingMount(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Missing mount
	mountReq := `{hello: {controller: "ctrl"}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for missing mount")
	}
	if resp.Error.Code != api.ErrCodeInvalidMessage {
		t.Errorf("expected error code %q, got %q", api.ErrCodeInvalidMessage, resp.Error.Code)
	}
}

func TestMountHandshake_UnmountOnDisconnect(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Mount
	mountReq := `{hello: {controller: "ctrl"}, mount: {path: "/users"}}` + "\n"
	if _, err := conn.Write([]byte(mountReq)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("mount failed: %s", resp.Error.Message)
	}

	// Verify mount exists
	if server.Mounts.Lookup("/users") == nil {
		t.Fatal("expected mount to be registered")
	}

	// Close connection
	conn.Close()

	// Wait for cleanup (poll with timeout)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.Mounts.Lookup("/users") == nil {
			return // Success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected mount to be unregistered after disconnect")
}

func TestMountHandshake_MultipleControllers(t *testing.T) {
	server := New(&Spec{})

	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	addr := server.TCPAddr()

	// Mount different paths from different controllers
	paths := []string{"/users", "/posts", "/comments"}
	conns := make([]net.Conn, len(paths))

	for i, path := range paths {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		conns[i] = conn
		defer conn.Close()

		mountReq := `{hello: {controller: "ctrl-` + path[1:] + `"}, mount: {path: "` + path + `"}}` + "\n"
		if _, err := conn.Write([]byte(mountReq)); err != nil {
			t.Fatalf("failed to write: %v", err)
		}

		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}

		var resp api.MountResponse
		if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("mount %s failed: %s", path, resp.Error.Message)
		}
	}

	// Verify all mounts
	mounts := server.Mounts.List()
	if len(mounts) != len(paths) {
		t.Errorf("expected %d mounts, got %d", len(paths), len(mounts))
	}

	for _, path := range paths {
		if server.Mounts.Lookup(path) == nil {
			t.Errorf("expected mount at %s", path)
		}
	}
}
