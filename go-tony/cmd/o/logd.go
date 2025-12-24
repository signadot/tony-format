package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type LogDConfig struct {
	*MainConfig
	LogD *cli.Command
}

func LogDCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &LogDConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.LogD, "logd").
		WithSynopsis("logd <subcommand>").
		WithDescription("logd storage server commands").
		WithSubs(
			LogDServeCommand(cfg),
			LogDClientCommand(cfg),
			LogDSessionCommand(cfg))
}

type LogDServeConfig struct {
	*LogDConfig
	Serve      *cli.Command
	DataDir    string `cli:"name=data desc='directory for logd data'"`
	ConfigFile string `cli:"name=config desc='configuration file (tony format)'"`
	Addr       string `cli:"name=addr desc='TCP listen address' default=localhost:9123"`
	HTTPPort   int    `cli:"name=http desc='HTTP server port (optional, disabled by default)'"`
}

func LogDServeCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDServeConfig{LogDConfig: logdCfg, Addr: "localhost:9123"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve -data <dir> [-addr <addr>] [-http <port>]").
		WithDescription("run the logd storage server (TCP by default, HTTP optional)").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			return logdServe(cfg, cc, args)
		})
}

func logdServe(cfg *LogDServeConfig, cc *cli.Context, args []string) error {
	_, err := cfg.Serve.Parse(cc, args)
	if err != nil {
		return err
	}

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// Load configuration
	var serverConfig *server.Config
	if cfg.ConfigFile != "" {
		serverConfig, err = server.LoadConfig(cfg.ConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Initialize storage
	s, err := storage.Open(cfg.DataDir, 0755, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create server
	srv := server.New(&server.Spec{
		Config:  serverConfig,
		Storage: s,
	})

	// Start TCP listener (default)
	if err := srv.StartTCP(cfg.Addr); err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	fmt.Fprintf(cc.Out, "TCP session listener on %s\n", srv.TCPAddr())
	defer srv.StopTCP()

	// Start HTTP server if port specified
	if cfg.HTTPPort > 0 {
		addr := fmt.Sprintf(":%d", cfg.HTTPPort)
		fmt.Fprintf(cc.Out, "HTTP server on %s\n", addr)
		return http.ListenAndServe(addr, srv)
	}

	// Block forever (TCP only mode)
	select {}
}

type LogDClientConfig struct {
	*LogDConfig
	Client *cli.Command
}

func LogDClientCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDClientConfig{LogDConfig: logdCfg}
	return cli.NewCommandAt(&cfg.Client, "client").
		WithSynopsis("client <addr>").
		WithDescription("send requests to logd server (newline-delimited wire format)").
		WithRun(func(cc *cli.Context, args []string) error {
			return logdClient(cfg, cc, args)
		})
}

func logdClient(cfg *LogDClientConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Client.Parse(cc, args)
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return fmt.Errorf("usage: client <addr>")
	}

	addr := args[0]
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}

	// Read requests from stdin, one per line (wire format)
	scanner := bufio.NewScanner(cc.In)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		// Parse the request to determine the method
		req, err := parse.Parse(line)
		if err != nil {
			return fmt.Errorf("failed to parse request: %w", err)
		}

		// Determine HTTP method from request structure
		// If patch: field exists at top level -> PATCH
		// Otherwise -> MATCH
		method := "MATCH"
		if hasField(req, "patch") {
			method = "PATCH"
		}

		// Send HTTP request
		httpReq, err := http.NewRequest(method, addr+"/api/data", bytes.NewReader(line))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/x-tony")

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		// Read response body
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		// Parse and re-encode response (to normalize format)
		respNode, err := parse.Parse(respBody)
		if err != nil {
			// If we can't parse, just output raw
			cc.Out.Write(respBody)
			cc.Out.Write([]byte("\n"))
			continue
		}

		// Encode response in wire format (one line)
		var buf bytes.Buffer
		if err := encode.Encode(respNode, &buf, encode.EncodeWire(true)); err != nil {
			return fmt.Errorf("failed to encode response: %w", err)
		}
		cc.Out.Write(buf.Bytes())
		cc.Out.Write([]byte("\n"))
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdin: %w", err)
	}

	return nil
}

// hasField checks if an object node has a field with the given name.
func hasField(node *ir.Node, name string) bool {
	if node == nil || node.Type != ir.ObjectType {
		return false
	}
	for _, f := range node.Fields {
		if f.ParentField == name {
			return true
		}
	}
	return false
}

type LogDSessionConfig struct {
	*LogDConfig
	Session *cli.Command
}

func LogDSessionCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDSessionConfig{LogDConfig: logdCfg}
	return cli.NewCommandAt(&cfg.Session, "session").
		WithSynopsis("session <addr>").
		WithDescription("connect to logd via TCP session protocol (supports watch)").
		WithRun(func(cc *cli.Context, args []string) error {
			return logdSession(cfg, cc, args)
		})
}

func logdSession(cfg *LogDSessionConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Session.Parse(cc, args)
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return fmt.Errorf("usage: session <addr>")
	}

	addr := args[0]

	// Connect via TCP
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	fmt.Fprintf(cc.Out, "Connected to %s\n", addr)

	// Create channels for coordination
	done := make(chan struct{})
	inputDone := make(chan struct{})

	// Start goroutine to read responses from server
	go func() {
		defer close(done)
		decoder, err := stream.NewDecoder(conn, stream.WithBrackets())
		if err != nil {
			fmt.Fprintf(cc.Out, "Error creating decoder: %v\n", err)
			return
		}

		for {
			// Read a complete document
			node, err := readSessionDocument(decoder)
			if err != nil {
				if err == io.EOF {
					return
				}
				fmt.Fprintf(cc.Out, "Read error: %v\n", err)
				return
			}

			if node == nil {
				continue
			}

			// Encode and print response
			var buf bytes.Buffer
			if err := encode.Encode(node, &buf); err != nil {
				fmt.Fprintf(cc.Out, "Encode error: %v\n", err)
				continue
			}
			cc.Out.Write(buf.Bytes())
			cc.Out.Write([]byte("\n"))
		}
	}()

	// Read requests from stdin and send to server
	go func() {
		defer close(inputDone)
		scanner := bufio.NewScanner(cc.In)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}

			// Send the line followed by newline
			if _, err := conn.Write(append(line, '\n')); err != nil {
				fmt.Fprintf(cc.Out, "Write error: %v\n", err)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(cc.Out, "Stdin error: %v\n", err)
		}
	}()

	// Wait for either done or inputDone
	select {
	case <-done:
		// Server closed connection
	case <-inputDone:
		// Stdin closed, wait a bit for final responses
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
	}

	return nil
}

// readSessionDocument reads events until we have a complete document.
func readSessionDocument(decoder *stream.Decoder) (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := decoder.ReadEvent()
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

		if started && decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
}
