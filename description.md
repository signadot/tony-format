# git-issue: phases beyond open/closed, as namespaces automation can enumerate and move through

# git-issue: phases beyond open/closed, as namespaces automation can enumerate and move through

## Context

verse (signadot/verse, `docs/working`) organises its design work on git-issue. A design item has a
**phase** (proposal → planned → landed, or overtaken / withdrawn), a **custody** (owner, until) and
**review** gates. verse's git-issue source reflects the tracker, and a charter would run the phase
machine over it: reminders, review gates, checks on a landing.

Today there are two statuses, so a phase could only be a label. A label set is not a state:
nothing stops two phases at once, and the property open/closed already have, "status is the
namespace, which cannot be wrong, and the descendant settles a race", does not reach it.

## What is needed

Layout (decided in discussion, 2026-09-22):

    refs/git-issues/v1/open/<xidr>             the issue's chain, as today
    refs/git-issues/v1/closed/<xidr>           the issue's chain, as today
    refs/git-issues/v1/phase/<phase>/<xidr>    the commit on that chain that entered <phase>

1. **Phase is its own ref, beside the chain.** The chain stays in `open/` or `closed/`; an issue's
   phase is where its phase ref is. The phase ref names the commit on the issue's chain that made
   the move, so it is always an ancestor of (or equal to) the issue's tip, and later edits leave it
   where it is. A phase move is one transaction: the move commit, the issue ref advanced to it, and
   the phase ref put at it and removed from the old phase, all in one `update-ref --stdin`, and
   pushed `--atomic`. So a clone holds at most one phase ref per issue, and nothing -- a move, a
   pull, a v1 client -- writes a second. Two phase refs for one issue in one clone, or a phase ref
   whose commit is not on the issue's chain, is a malformed repository: a reader reports it, and a
   cleanup pass (as `CleanupStaleRefs` is for status) repairs it.
2. **Phase and status are orthogonal.** A phase never implies `open` or `closed`, and a close or
   reopen never moves a phase. The machine governs phase only; status stays what it is today. So a
   v1 client that closes or reopens an issue with a phase contradicts nothing, and nothing need
   reconcile the two. Automation that wants a terminal phase to close the issue does both moves.
3. **A declared machine, per repository.** The phases and the allowed transitions are declared
   once, versioned in the repository. `git issue` refuses an undeclared phase or transition, and
   each phase says whether it is terminal (for the machine: no transition out). Automation reads
   the declaration rather than hard-coding a list.
4. **Transitions readable without prose.** The move commit carries a transition entry in
   meta.tony (from, to), and its author and date are who and when, so a reflecting source can say
   "entered <phase> at <t>, by <who>" from the phase ref alone, without reading messages.
5. **Enumerable by readers, never named literally.** issuelib exports the phase prefix and a
   `PhaseFromRef`, so a client lists what the library says exists. This is the lesson from
   2026-09-22: verse's source listed `refs/issues/` and `refs/closed/` as literals, went blind
   when trackers moved to `refs/git-issues/v1/` (go-tony v0.0.230), and its tests failed after
   verse bumped to v0.0.232 with nobody noticing. issuelib's own doc names the mechanism: a
   client names its refspecs literally, so it cannot see a ref it does not name.
6. **v1 compatibility, without a new generation.** A v1 client names `open/` and `closed/` only,
   in listing, lookup and push, and its push removes only the other status and gen0 refs. So it
   still sees every issue, never deletes a phase ref, and a reflecting reader tombstones nothing.
   What it cannot do is carry phase refs: a phase travels only through phase-aware clones. That
   is a degradation, not a loss, and with (2) there is no state a v1 write can contradict.
7. **Sync carries phase.** push, pull and the lease treat the phase ref as they treat the issue
   ref, with tracking copies under `remotes/<remote>/phase/`. Clones disagree on a phase when each
   moved the issue and neither move descends from the other; each clone still has one phase. Pull
   merges the chain as it does today, keeps this clone's phase ref, and reports the conflict; push
   refuses, as it refuses a failed lease. Never last writer wins. The conflict is resolved by a
   phase move made on top of the merge: that move descends from both, so from then on the
   descendant rule settles it everywhere.
8. **The CLI:** a move to a phase, `list` filtered by phase, and `show` with the phase history.

## Deliberately out of scope

- **Custody** (owner, until) is orthogonal to phase: fields in meta.tony, not a namespace.
- **Review** is a gate over a transition. The automation poses it (verse does); git-issue need
  not model it.