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

1. **Phases as namespaces.** An issue is in exactly one phase, and its phase is where its ref is.
   A phase move is the same atomic ref move a close is, a commit on the issue's chain, so the
   descendant rule settles a race between any two phases, not only open and closed.
2. **A declared machine, per repository.** The phases and the allowed transitions are declared
   once, versioned in the repository. `git issue` refuses an undeclared phase or transition, and
   each phase says whether it is terminal (today's `closed` is the terminal case). Automation
   reads the declaration rather than hard-coding a list.
3. **Transitions readable without prose.** Each move records from, to, who and when in a form a
   reader parses (a transition entry in meta.tony, or a structured trailer on the commit), so a
   reflecting source can say "entered <phase> at <t>, by <who>" without reading messages.
4. **Enumerable by readers, never named literally.** issuelib exports the phase prefixes and a
   `PhaseFromRef`, so a client lists what the library says exists. This is the lesson from
   2026-09-22: verse's source listed `refs/issues/` and `refs/closed/` as literals, went blind
   when trackers moved to `refs/git-issues/v1/` (go-tony v0.0.230), and its tests failed after
   verse bumped to v0.0.232 with nobody noticing. issuelib's own doc names the mechanism: a
   client names its refspecs literally, so it cannot see a ref it does not name.
5. **Compatibility is the design question.** A v1 reader lists only `open/` and `closed/`, so an
   issue moved to a new phase namespace under v1 disappears for it, and a reflecting reader
   tombstones what it stops seeing, which is worse than missing it. Either phases come with a new
   generation and adoption, as gen0 → v1 did, or with a layout that keeps v1 readers complete.
   That needs deciding before any phase namespace is written.
6. **Sync carries phase.** push, pull, the lease and the merge treat a phase move as they treat
   close and reopen: one issue moved to different phases on two sides is a divergence resolved by
   the same rules, never last writer wins.
7. **The CLI:** a move to a phase, `list` filtered by phase, and `show` with the phase history.

## Deliberately out of scope

- **Custody** (owner, until) is orthogonal to phase: fields in meta.tony, not a namespace.
- **Review** is a gate over a transition. The automation poses it (verse does); git-issue need
  not model it.