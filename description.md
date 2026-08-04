# o sys: serve pprof by default on a known admin address — gops is present, but was unreachable on the one daemon that needed a stack dump

`o sys` server commands should serve pprof by default, on a known address. Today the only
live-introspection channel is the gops agent, and today it was not there when it was needed.

gops is already started, unconditionally, in all three server commands — `o sys up`
(`cmd/o/system_compose.go:56`), `o logd serve` (`cmd/o/logd.go:64`) and `o docd serve`
(`cmd/o/docd.go:111`), each calling `agent.Listen(agent.Options{})`. It works: a freshly
started `o sys up` writes its port file, and `gops stack <pid>` returns a full goroutine dump.
So the gops half of this is done, and the request is really about the two gaps around it.


The first gap is that the channel is not robust. A daemon in the wedged state described in
`hw4y878ph12ksfbwd1n0` — every verse `List` returning `[]` while `verse status` reported a
healthy rev — had a gops agent that could not be reached:

    $ gops stack 53830
    couldn't get port by PID: dial tcp 127.0.0.1:63076: connect: connection refused

    $ lsof -nP -p 53830 -a -iTCP | grep 63076
    o  53830 scott  3u  IPv4  TCP 127.0.0.1:63076 (LISTEN)

The port file was written at startup and holds the right port. The socket is still in LISTEN.
Connections are refused anyway, so nothing is accepting. A freshly started daemon answered the
identical command, so this is the wedged process, not the setup. The one channel that could
have said what those goroutines were doing was the one thing that had also stopped working,
and there was no second way in: the process had to be killed with `SIGQUIT` to get a stack,
which destroys the state being investigated. A diagnostic channel that shares a failure mode
with the thing it diagnoses is not one you can rely on when it matters.

The second gap is reach. `agent.Options{}` binds `127.0.0.1` on an ephemeral port, recorded in
a per-pid file under the invoking user's config dir. That is fine on a laptop and unusable
anywhere else: no fixed address, nothing to scrape, nothing reachable from outside a container,
and no access at all for an operator who is not the user that started the daemon.

Neither gap is closed by adding a flag people have to know to pass. Something that is only on
when you thought to ask for it is not there during the incident you did not expect.


What is missing, concretely:

- `net/http/pprof` served by default from the server commands, on an admin listener with a
  known address (`--admin-addr`, defaulted, disable-able). That also gets the mutex, block and
  goroutine-count profiles gops does not expose, and makes the daemon scrapeable by ordinary
  tooling.
- The admin listener kept off the data path, so it can answer while logd/docd sessions cannot.
  The wedge above is the argument: the interesting stacks are exactly the ones a working data
  path would not have needed.
- The chosen addresses reported — logged at startup and echoed by `o sys` — rather than left
  in a pid file the operator has to know to look for. Today, finding gops meant noticing an
  unexplained listener in `lsof` and guessing.

One related note, since it cost a session's worth of confusion: `logd`, the docd client port
and the docd mount port all accept a TCP connection and then parse whatever arrives as a tony
document. An HTTP request to any of them — a reasonable thing to try when hunting for pprof —
is read as a document, fails to decode, and kills the session:

    session tcp-7    read error: key not in obj
    client session   key not in obj
    session docd-2   invalid mount request: expected map for MountRequest, got String

Reproduced deliberately against a scratch daemon. Costing a session for a garbage first frame
may be the right call, but it is worth deciding on purpose. A real pprof address would remove
the reason anyone points HTTP at those ports in the first place.

Reported by scott from operating verse.