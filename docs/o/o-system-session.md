# o system session

Open a session to [logd](https://signadot.github.io/tony-format/logd/) or [docd](https://signadot.github.io/tony-format/docd/) and speak its protocol by hand.

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

The requests -- hello, match, patch, newtx, watch, unwatch, [schema](https://signadot.github.io/tony-format/tonyschema/), ping -- and
what each one answers are at <https://signadot.github.io/tony-format/logd/session/>

## Usage

```
o system session <addr>
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | colorize; on by default to a terminal, -color=false to suppress |
| `-wire` | bool |  | output in compact format |
| `-h`, `-help` | bool |  | show help for this command |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [`o system`](o-system.md)
- [`o system logd`](o-system-logd.md)
- [`o system docd`](o-system-docd.md)
- [`o system up`](o-system-up.md)

