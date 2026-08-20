package libctl

import (
	"fmt"
	"net"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// MountClient manages a controller's connection to docd.
type MountClient struct {
	conn    net.Conn
	decoder *stream.Decoder

	// Mount state (set after successful Mount)
	docdID   string
	path     string
	logdAddr string

	// LogdSession for writing results (created if LogdAddr provided)
	logd *LogdSession
}

// MountConfig contains configuration for mounting to docd.
type MountConfig struct {
	// DocdAddr is the address of docd (e.g., "localhost:9090")
	DocdAddr string

	// LogdAddr is the address of logd (e.g., "localhost:9091")
	// Controllers use this to write results directly to logd.
	LogdAddr string

	// Controller is the identifier for this controller
	Controller string

	// Path is the path to mount (e.g., "/users")
	Path string

	// Schema is the optional schema for this mount
	Schema *ir.Node

	// DialTimeout is the timeout for connecting (default: 5s)
	DialTimeout time.Duration

	// ForceAfter, when non-nil, overrides how long docd waits for overlapping
	// watch readers to drain before force-ending them so this mount can proceed. A
	// pointer to 0 means wait forever (never force); nil uses docd's default.
	ForceAfter *time.Duration
}

// Mount connects to docd and performs the mount handshake.
// Returns a MountClient on success that can be used for subsequent operations.
func Mount(cfg *MountConfig) (*MountClient, error) {
	if cfg.DocdAddr == "" {
		return nil, fmt.Errorf("DocdAddr is required")
	}
	if cfg.Controller == "" {
		return nil, fmt.Errorf("Controller is required")
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("Path is required")
	}

	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}

	// Connect to docd
	conn, err := net.DialTimeout("tcp", cfg.DocdAddr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docd: %w", err)
	}

	// Create client
	client := &MountClient{
		conn:     conn,
		path:     cfg.Path,
		logdAddr: cfg.LogdAddr,
	}

	// Perform handshake
	if err := client.handshake(cfg); err != nil {
		conn.Close()
		return nil, err
	}

	// Create logd session if address provided
	if cfg.LogdAddr != "" {
		client.logd = NewLogdSession(&LogdSessionConfig{
			Addr:     cfg.LogdAddr,
			ClientID: cfg.Controller,
		})
	}

	return client, nil
}

// handshake sends the mount request and processes the response.
func (c *MountClient) handshake(cfg *MountConfig) error {
	// Create decoder for responses
	decoder, err := stream.NewDecoder(c.conn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}
	c.decoder = decoder

	// Build mount request
	mount := &api.MountSpec{
		Path:   cfg.Path,
		Schema: cfg.Schema,
	}
	if cfg.ForceAfter != nil {
		fa := cfg.ForceAfter.String() // "0s" (wait forever) or e.g. "5s"
		mount.ForceAfter = &fa
	}
	req := &api.MountRequest{
		Hello: &api.MountHello{Controller: cfg.Controller, Protocol: logdapi.ProtocolVersion},
		Mount: mount,
	}

	// Send request
	if err := c.sendRequest(req); err != nil {
		return fmt.Errorf("failed to send mount request: %w", err)
	}

	// Read response
	resp, err := c.readResponse()
	if err != nil {
		return fmt.Errorf("failed to read mount response: %w", err)
	}

	// Check for error
	if resp.Error != nil {
		return resp.Error
	}

	// Validate response
	if resp.Result == nil {
		return fmt.Errorf("invalid response: missing result")
	}
	if resp.Result.Hello == nil {
		return fmt.Errorf("invalid response: missing hello")
	}
	if resp.Result.Mount == nil {
		return fmt.Errorf("invalid response: missing mount")
	}
	if !resp.Result.Mount.Accepted {
		return fmt.Errorf("mount not accepted")
	}

	c.docdID = resp.Result.Hello.DocdID
	return nil
}

// sendRequest sends a mount request to docd.
func (c *MountClient) sendRequest(req *api.MountRequest) error {
	data, err := req.ToTony(logdapi.WireOptions()...)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}
	return nil
}

// readResponse reads a mount response from docd.
func (c *MountClient) readResponse() (*api.MountResponse, error) {
	node, err := stream.ReadDocument(c.decoder)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("empty response")
	}

	var resp api.MountResponse
	if err := resp.FromTonyIR(node); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// DocdID returns the docd server ID from the handshake.
func (c *MountClient) DocdID() string {
	return c.docdID
}

// Path returns the mounted path.
func (c *MountClient) Path() string {
	return c.path
}

// LogdAddr returns the logd address for writing results.
func (c *MountClient) LogdAddr() string {
	return c.logdAddr
}

// Logd returns the logd session for writing results.
// Returns nil if LogdAddr was not provided in MountConfig.
// The session connects lazily on first use.
func (c *MountClient) Logd() *LogdSession {
	return c.logd
}

// Conn returns the underlying connection for advanced use.
// The connection is shared with the decoder, so care must be taken
// when reading directly from it.
func (c *MountClient) Conn() net.Conn {
	return c.conn
}

// Close closes the connection to docd and the logd session.
func (c *MountClient) Close() error {
	if c.logd != nil {
		c.logd.Close()
	}
	return c.conn.Close()
}

// Unmount gracefully unmounts the controller's subtree: it asks docd to drain the
// watches overlapping the mount (force-ending them after forceAfter so they see
// session_unmounted rather than an abrupt controller_unavailable) and fully
// remove the mount — no tombstone — then waits for docd to close the connection,
// which signals completion. forceAfter is as MountConfig.ForceAfter: a pointer to
// 0 waits forever, nil uses docd's default.
//
// The MountClient's connection must not be concurrently read by a controller
// runtime while Unmount runs, since Unmount reads it to completion.
func (c *MountClient) Unmount(forceAfter *time.Duration) error {
	spec := &api.UnmountSpec{}
	if forceAfter != nil {
		fa := forceAfter.String() // "0s" = wait forever
		spec.ForceAfter = &fa
	}
	if err := c.sendRequest(&api.MountRequest{Unmount: spec}); err != nil {
		return fmt.Errorf("failed to send unmount: %w", err)
	}
	// docd drains, removes the mount, then closes the connection; EOF is completion.
	for {
		if _, err := stream.ReadDocument(c.decoder); err != nil {
			return nil
		}
	}
}
