# logd: a hello refused for its protocol version leaves the session open, and every later request is answered

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

On a fresh connection `{hello: {clientId: ahead, protocol: 99}}` gets protocol_mismatch;
a patch sent next on the same connection commits (commit 1), a match is answered, and
a watch is established.

There is no handshake gate: Session keeps no handshake state, dispatch
(server/session.go:277) routes every operation unconditionally, and handleHello
(:346-352) sends the error and returns without recording anything. (A client that never
sends hello is served, deliberately: 3ea003f accepts protocol-0 clients.)

Consequence: a pipelining client, or one that does not treat the refusal as fatal, has
its reads and writes answered by a server that told it they would not be -- the "wrong
answer looks like success" the version check exists to prevent (ProtocolVersion's doc).

Neighbour, not a duplicate: k0d4y1m6h12kr7cdgdn0 (misplaced request fields ignored).