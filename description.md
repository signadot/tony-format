# o: the session client is o system session, not o system logd session, and its help shows a session

`o system logd session <addr>` sat under logd, but it is a client of the session
protocol, which docd speaks verbatim: a docd address works the same. Filing it under
logd says the wrong thing about what it talks to. It is now `o system session <addr>`
(`o sys session`), a direct child of `system` beside logd, docd and up.

Its help said "connect to logd via TCP session protocol (supports watch)" and
nothing else -- not what to type first, not what comes back, not where the protocol
is written down. It now explains the wire shape (one request per stdin line, every
server document printed as it arrives), shows a hello / patch / watch / patch
transcript taken from a real logd, shows the piped-file form, and links
https://signadot.github.io/tony-format/logd/session/ for the request set.

The command moved out of cmd/o/logd.go into cmd/o/session.go unchanged. The -h
wiring test, docs/logd/session.md, system/logd/doc.go and logd/testdata/README.md
follow the new path, and docs/o/ is regenerated: o-system-logd-session.md is gone
(o docs does not remove stale pages) and o-system-session.md replaces it.