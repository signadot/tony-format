package commands

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

// hasIPv6Loopback says whether this machine has ::1 to listen on.
func hasIPv6Loopback() bool {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// TestListenOn_LocalhostIsBothAddresses: localhost is listened on at
// 127.0.0.1 and at ::1, one port, so the name reaches serve however it
// resolves; and the URL answered is the name's (rmgg0b9fh12kr235q1n0).
func TestListenOn_LocalhostIsBothAddresses(t *testing.T) {
	lns, at, err := listenOn("localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: newIssueServer(testRepo(t))}
	for _, ln := range lns {
		go srv.Serve(ln)
	}
	t.Cleanup(func() { srv.Close() })

	_, port, _ := net.SplitHostPort(lns[0].Addr().String())
	reach := []string{"http://127.0.0.1:" + port + "/"}
	if hasIPv6Loopback() {
		if len(lns) != 2 {
			t.Fatalf("listening on %d address(es), want both", len(lns))
		}
		if at != "http://localhost:"+port+"/" {
			t.Errorf("the URL is %s", at)
		}
		reach = append(reach, "http://[::1]:"+port+"/")
	}
	for _, target := range append(reach, at) {
		res, err := http.Get(target)
		if err != nil {
			t.Errorf("%s: %v", target, err)
			continue
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s answered %d", target, res.StatusCode)
		}
	}
}

// TestListenOn_RefusesAnAddressThatAnswers: another program that has the
// port, on one loopback address or on a wildcard, is a refusal that names
// the address -- where a bind alone would have succeeded beside it.
func TestListenOn_RefusesAnAddressThatAnswers(t *testing.T) {
	others := []string{"127.0.0.1:0"}
	if hasIPv6Loopback() {
		others = append(others, "[::1]:0", "[::]:0")
	}
	for _, other := range others {
		taken, err := net.Listen("tcp", other)
		if err != nil {
			t.Fatalf("%s: %v", other, err)
		}
		_, port, _ := net.SplitHostPort(taken.Addr().String())
		lns, _, err := listenOn("localhost:" + port)
		if err == nil {
			for _, ln := range lns {
				ln.Close()
			}
			t.Errorf("listened on localhost:%s beside a program on %s", port, other)
		} else if !strings.Contains(err.Error(), port) || !strings.Contains(err.Error(), "already answers") {
			t.Errorf("beside a program on %s: %v", other, err)
		}
		taken.Close()
	}
	// An address given is listened on as given, and refused the same way.
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	if _, _, err := listenOn(taken.Addr().String()); err == nil || !strings.Contains(err.Error(), "already answers") {
		t.Errorf("an address that answers: %v", err)
	}
}
