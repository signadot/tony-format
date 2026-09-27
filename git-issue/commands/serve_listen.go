package commands

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// Where serve listens. An address is listened on as given, but for the name
// localhost, which is two addresses: 127.0.0.1 and ::1. Go binds the first it
// resolves, and a browser tries ::1 first, so a serve on one of them is
// reached or not by which the name comes to -- and another program on the
// other answers in its place (rmgg0b9fh12kr235q1n0). So localhost is both,
// and either already answering is a refusal.

// listenOn listens on addr, and answers the URL to reach what it serves at.
func listenOn(addr string) ([]net.Listener, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	if host != "localhost" {
		ln, err := listenIfFree("tcp", addr)
		if err != nil {
			return nil, "", err
		}
		return []net.Listener{ln}, "http://" + ln.Addr().String() + "/", nil
	}
	four, err := listenIfFree("tcp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, "", err
	}
	// The port the first took is the port of both: port 0 is any port.
	_, port, _ = net.SplitHostPort(four.Addr().String())
	six, err := listenIfFree("tcp6", net.JoinHostPort("::1", port))
	switch {
	case err == nil:
		return []net.Listener{four, six}, "http://localhost:" + port + "/", nil
	case errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EAFNOSUPPORT):
		// No IPv6 loopback here: localhost is the one address.
		return []net.Listener{four}, "http://" + four.Addr().String() + "/", nil
	}
	four.Close()
	return nil, "", err
}

// listenIfFree listens on an address nothing answers at. A bind does not find
// every program that has the port: one listening on a wildcard address
// leaves a more specific bind to succeed, on macOS, and the two then share
// the port by address. So it asks first.
func listenIfFree(network, addr string) (net.Listener, error) {
	if _, port, _ := net.SplitHostPort(addr); port != "0" {
		if conn, err := net.DialTimeout(network, addr, time.Second); err == nil {
			conn.Close()
			return nil, fmt.Errorf("failed to listen on %s: something already answers there; --addr takes another", addr)
		}
	}
	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	return ln, nil
}
