# logd: a client session dies on "bad literal", always at col=4084

# logd: a client session dies on "bad literal", always at col=4084

Driving verse against a local `verse up` (logd + docd spawned as children, go-tony at the
version verse pins today), the store session died three times with a parse error, and the
column is the same every time while the offset and line are not:

    "client session error" session=client-3 error="bad literal at `...?...` at offset 148554 (line=178, col=4084)"
    "session error"        session=tcp-4    error="read error: ... connection reset by peer"

    "client session error" session=client-4 error="bad literal at `...?...` at offset 5436 (line=9, col=4084)"
    "client session error" session=client-5 error="bad literal at `...?...` at offset 6971 (line=21, col=4084)"

Three different documents, three different offsets, three different lines, **col=4084 every
time**. That reads like a read-buffer boundary rather than anything about the content: a
literal that spans the end of one 4096-byte fill is scanned as though the buffer end were the
document end. `...?...` in the message is the parser's rendering of what it stopped on.

Each one resets the client's TCP session. verse reconnects immediately (a new `logd-session`
appears in the next log line), so the damage is bounded — but during the window every read
answers

    the store could not be read, so this answer would be an empty verse rather than the verse:
    connection lost: EOF — nothing is missing until it reads again, and writes may still be appending

and one read that failed that way was for an entity that was there.

## What was happening

Two of the three landed while writing charter rule records into a `verse connect` scope
(`verse charter add --scope dev1`, then `charter remove` — records stored under `!raw`, so
they are structure-carrying documents rather than short payloads). The third landed on a
`state put` of an entity with a single ~8000-character string value.

## Not reproducible on demand yet

I could not turn it into a recipe, and the failed attempts are worth recording so nobody
repeats them:

- string values of 3000, 4000, 4070, 4080, 4090, 4200, 5000, 6000, 8000, 12000, 20000
  characters, written and read back — no error, including the 8000 that produced one earlier;
- the same, with a rule actively watching that (system, kind), so the value rides a watch
  delivery rather than only a write;
- six scoped `charter add` + `charter remove` cycles;
- whole-verse reads, baseline and scoped, over a store holding a 12000-character value.

So it is not simply "a value longer than the buffer". The constant column says the boundary is
real; what has to be true of the bytes AT that boundary is the open question, and whoever knows
the scanner will see it faster than a black-box sweep will.

## What verse sees, for whoever picks this up

Reactivity recovers: after the drop, both a baseline rule and a scoped rule fired on the next
write, which is the level-triggered catch-up doing its job. So this is availability and not
correctness as far as the engine is concerned. The part that is not fine is that a read during
the window fails while the data is there, and nothing above the store can tell that window
from a real outage.