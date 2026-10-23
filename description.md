# session wire: a bare `!delete` before `}` stalls the decoder, waiting for a value

Over a session connection, `{id: w2, scratch: {write: {path: x, data: {n: 2, note: !delete}}}}` is never answered: the request loop's stream decoder waits for a value after the tag, and the blank-line document separator does not end it. The same text parses with `parse.Parse` (the storage tests use `{n: 3, note: !delete}` as a patch), and `!delete null` on the wire works.

Seen while testing scratch (r482sz5rh12ksj0cvdn0) with the server package's `newLiveSession` mock connection: the first request on the session was answered, the one holding the bare `!delete` was not, and nothing after it was read. Not verified against a real TCP session; the mock writes each request followed by a blank line.

Either the stream decoder should take a tag with no value as the tag on null, as the parser does, or the session docs should say a tag on the wire needs its value spelled.