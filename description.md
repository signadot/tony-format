# o diff -loop: a run that prints nothing, or fails, is a nil document and a panic

`o d -loop <cmd>` reads the command's stdout, parses it, and diffs it against the last run.
A run that prints NOTHING parses to a nil node with no error, and a run that FAILS is the
same thing, because cmd.Wait is never asked: diffLoop reads the pipe, parses, and diffs.
DiffWith(last, nil) then dereferences the nil in differ.diff (diff.go:386).

    o d -loop true  -loopEvery 1s -loopLim 2     # panics
    o d -loop false -loopEvery 1s -loopLim 2     # panics

Where it bit: `o d -loop 'verse -addr … data signadot' -loopUntil …` watching a drive on
staging. One iteration's `verse` call came back empty — a transient the loop exists to sit
through — and the watch died with a stack trace at the moment it was worth having.

A run that produced nothing is not a document that changed to nothing. Two readings, either
better than the panic:

- treat an empty or failed run as a MISSED iteration: say so on stderr, keep `last`, count
  it against -loopLim, and go round again. This is what a watch wants — the thing being
  watched was briefly unreachable, and the next answer is what to diff against;
- or exit non-zero naming the exit status and that nothing was written, the way an
  undecodable output already exits with "error decoding command output".

The first is the one -loopUntil wants: "stop once the output matches" is a promise about
the command's output, and an output that is not there is neither a match nor a mismatch.
Either way cmd.Wait belongs in the loop, so a command that dies is reported as having died
rather than as having said nothing.

Seen on o v0.0.204; go-tony/cmd/o/diff.go at 523944b has the same shape.