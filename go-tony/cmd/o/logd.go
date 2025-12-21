package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
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
			LogDClientCommand(cfg))
}

type LogDServeConfig struct {
	*LogDConfig
	Serve   *cli.Command
	DataDir string `cli:"name=data desc='directory for logd data'"`
	Port    int    `cli:"name=port desc='HTTP server port' default=9000"`
}

func LogDServeCommand(logdCfg *LogDConfig) *cli.Command {
	cfg := &LogDServeConfig{LogDConfig: logdCfg, Port: 9000}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Serve, "serve").
		WithSynopsis("serve -data <dir> [-port <port>]").
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

	if cfg.DataDir == "" {
		return fmt.Errorf("-data is required")
	}

	// Initialize storage
	s, err := storage.Open(cfg.DataDir, 0755, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create server
	srv := server.New(&server.Config{Storage: s})

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Fprintf(cc.Out, "Starting logd server on %s\n", addr)
	return http.ListenAndServe(addr, srv)
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
