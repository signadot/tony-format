package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/gops/agent"
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
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
			LogDSessionCommand(cfg))
}

type LogDServeConfig struct {
	*LogDConfig
	Serve      *cli.Command
	DataDir    string `cli:"name=data desc='directory for logd data'"`
	ConfigFile string `cli:"name=config desc='configuration file (tony format)'"`
	Addr       string `cli:"name=addr desc='TCP listen address' default=localhost:9123"`
}

func LogDServeCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDServeConfig{LogDConfig: logdCfg, Addr: "localhost:9123"}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve -data <dir> [-addr <addr>]").
		WithDescription("run the logd storage server").
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

	// Start gops agent for debugging
	if err := agent.Listen(agent.Options{}); err != nil {
		fmt.Fprintf(cc.Out, "gops agent failed: %v\n", err)
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
	s, err := storage.Open(cfg.DataDir, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create server
	srv := server.New(&server.Spec{
		Config:  serverConfig,
		Storage: s,
	})

	// Start TCP listener
	if err := srv.StartTCP(cfg.Addr); err != nil {
		return fmt.Errorf("failed to start TCP listener: %w", err)
	}
	fmt.Fprintf(cc.Out, "TCP session listener on %s\n", srv.TCPAddr())
	defer srv.StopTCP()

	// Block forever
	select {}
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
