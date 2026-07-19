package libctl

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/docd/server"
)

func TestMount_Success(t *testing.T) {
	// Start docd server
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	// Mount
	client, err := Mount(&MountConfig{
		DocdAddr:   srv.TCPAddr(),
		Controller: "test-ctrl",
		Path:       "/users",
	})
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer client.Close()

	// Verify state
	if client.DocdID() == "" {
		t.Error("expected DocdID to be set")
	}
	if client.Path() != "/users" {
		t.Errorf("expected path '/users', got %q", client.Path())
	}

	// Verify server registered the mount
	entry := srv.Mounts.Lookup("/users")
	if entry == nil {
		t.Fatal("expected mount to be registered on server")
	}
	if entry.Controller != "test-ctrl" {
		t.Errorf("expected controller 'test-ctrl', got %q", entry.Controller)
	}
}

func TestMount_WithSchema(t *testing.T) {
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	// Parse a schema
	schema, err := parse.Parse([]byte(`{define: {User: {id: .string, name: .string}}, accept: {users: .array(.User)}}`))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	client, err := Mount(&MountConfig{
		DocdAddr:   srv.TCPAddr(),
		Controller: "user-ctrl",
		Path:       "/users",
		Schema:     schema,
	})
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer client.Close()

	// Verify schema was stored
	entry := srv.Mounts.Lookup("/users")
	if entry == nil {
		t.Fatal("expected mount to be registered")
	}
	if entry.Schema == nil {
		t.Error("expected schema to be stored")
	}
}

func TestMount_PathAlreadyMounted(t *testing.T) {
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	// First mount succeeds
	client1, err := Mount(&MountConfig{
		DocdAddr:   srv.TCPAddr(),
		Controller: "ctrl-1",
		Path:       "/users",
	})
	if err != nil {
		t.Fatalf("first Mount failed: %v", err)
	}
	defer client1.Close()

	// Second mount to same path fails
	client2, err := Mount(&MountConfig{
		DocdAddr:   srv.TCPAddr(),
		Controller: "ctrl-2",
		Path:       "/users",
	})
	if err == nil {
		client2.Close()
		t.Fatal("expected error for duplicate mount")
	}
	t.Logf("Got expected error: %v", err)
}

func TestMount_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *MountConfig
	}{
		{"missing DocdAddr", &MountConfig{Controller: "ctrl", Path: "/users"}},
		{"missing Controller", &MountConfig{DocdAddr: "localhost:9090", Path: "/users"}},
		{"missing Path", &MountConfig{DocdAddr: "localhost:9090", Controller: "ctrl"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Mount(tt.cfg)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestMount_ConnectionRefused(t *testing.T) {
	// Try to connect to a port that's not listening
	_, err := Mount(&MountConfig{
		DocdAddr:   "127.0.0.1:1", // Port 1 is unlikely to be listening
		Controller: "ctrl",
		Path:       "/users",
	})
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestMount_CleanupOnClose(t *testing.T) {
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	client, err := Mount(&MountConfig{
		DocdAddr:   srv.TCPAddr(),
		Controller: "ctrl",
		Path:       "/users",
	})
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}

	// Verify mount is live
	if !srv.Mounts.Lookup("/users").Live() {
		t.Fatal("expected mount to be registered")
	}

	// Close client
	client.Close()

	// After close the mount is tombstoned (present but not live), not removed.
	for i := 0; i < 100; i++ {
		if e := srv.Mounts.Lookup("/users"); e != nil && !e.Live() {
			return // Success: tombstoned
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected mount to be tombstoned after close")
}

func TestMount_MultipleControllers(t *testing.T) {
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	paths := []string{"/users", "/posts", "/comments"}
	clients := make([]*MountClient, len(paths))

	// Mount all paths
	for i, path := range paths {
		client, err := Mount(&MountConfig{
			DocdAddr:   srv.TCPAddr(),
			Controller: "ctrl-" + path[1:],
			Path:       path,
		})
		if err != nil {
			t.Fatalf("Mount %s failed: %v", path, err)
		}
		clients[i] = client
		defer client.Close()
	}

	// Verify all mounts
	for _, path := range paths {
		if srv.Mounts.Lookup(path) == nil {
			t.Errorf("expected mount at %s", path)
		}
	}
}
