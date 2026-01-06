package libctl

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
)

// MountClient manages a controller's connection to docd.
type MountClient struct {
	conn    net.Conn
	decoder *stream.Decoder

	// Mount state (set after successful Mount)
	docdID   string
	path     string
	logdAddr string
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
	req := &api.MountRequest{
		Hello: &api.MountHello{
			Controller: cfg.Controller,
		},
		Mount: &api.MountSpec{
			Path:   cfg.Path,
			Schema: cfg.Schema,
		},
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
	data, err := req.ToTony(gomap.EncodeWire(true))
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
	node, err := c.readDocument()
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

// readDocument reads events until we have a complete document.
func (c *MountClient) readDocument() (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := c.decoder.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if len(events) > 0 {
					return stream.EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		started = true

		if started && c.decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
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

// Conn returns the underlying connection for advanced use.
// The connection is shared with the decoder, so care must be taken
// when reading directly from it.
func (c *MountClient) Conn() net.Conn {
	return c.conn
}

// Close closes the connection to docd.
func (c *MountClient) Close() error {
	return c.conn.Close()
}
