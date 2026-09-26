# the comment command hands ops a ref where it takes an id, so every comment from the command line is refused

## What happens

Since v0.1.0, `git issue comment <id> text` answers

    issue not found: refs/git-issues/v1/open/<xidr>

and writes nothing. The command resolves the id to a ref, which it needs for the editor's
context directory, and then hands that ref to `ops.Comment`, which resolves an id or a prefix
and not a ref: `FindRef("refs/...")` finds nothing. The MCP tool `issue_comment` is not affected,
since it calls ops with an id.

## Why no test caught it

The ops refactor (1k3x9sp6h12ks5c0pxn0) kept the command tests as the proof that behaviour was
unchanged, and they were green -- because none of them calls the comment command. The sync
tests add comments through a store helper (`comment()` in sync_test.go), which writes the file
directly. The command itself had no test. It was found by driving the released binary from a
shell beside the MCP server, in the umbrella's scenario.

## The fix

The command passes the id it was given to `ops.Comment`, and keeps the ref for the editor's
context. A test runs the command itself and reads the comment back.