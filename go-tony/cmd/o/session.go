package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

const sessionDesc = `Open a session to logd or docd and speak its protocol by hand.

The session protocol is newline-delimited tony documents in both directions: each
line on stdin is one request, sent as written, and every document the server
sends back -- results, watch events, errors -- is printed as it arrives. docd
speaks the same protocol as logd, so the address is the only thing that changes.

The first request is a hello, which names the client. Here a patch is written,
a watch is placed, and a second patch arrives at the watch as an event:

    $ o sys session localhost:9123
    Connected to localhost:9123
    {hello: {clientId: probe}}
    {result: {hello: {protocol: 3 schemaCommit: 0 serverId: tcp-1}}}
    {patch: {path: a.b, data: {status: ready}}}
    {result: {patch: {commit: 1 data: {status: ready}}}}
    {watch: {path: a}}
    {result: {watch: {watching: a}}}
    {event: {commit: 1 path: a state: {b: {status: ready}}}}
    {patch: {path: a.b, data: {status: done}}}
    {result: {patch: {commit: 2 data: {status: done}}}}
    {event: {commit: 2 patch: {b: {status: done}} path: a}}

A watch keeps the session open and prints each change as it commits. A file of
requests can be piped in instead of typing them:

    cat requests.tony | o sys session localhost:9123

The requests -- hello, match, patch, newtx, watch, unwatch, schema, ping -- and
what each one answers are at https://signadot.github.io/tony-format/logd/session/`

type SessionConfig struct {
	*MainConfig
	Session *cli.Command
}

func SessionCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &SessionConfig{MainConfig: mainCfg}
	return cli.NewCommandAt(&cfg.Session, "session").
		WithSynopsis("session <addr>").
		WithDescription(sessionDesc).
		WithRun(func(cc *cli.Context, args []string) error {
			return session(cfg, cc, args)
		})
}

func session(cfg *SessionConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Session.Parse(cc, args)
	if err != nil {
		return err
	}
	if helpAsked(cfg.Session, cc, cfg.Help) {
		return nil
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
			if err := encode.Encode(node, &buf, encode.EncodeWire(true)); err != nil {
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
