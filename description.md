# git-issue: key=value labels and a merge that knows removals, so automation can drive phases

# git-issue: key=value labels and a merge that knows removals, so automation can drive phases

## Context

verse (signadot/verse, `docs/working`) organises its design work on git-issue. A design item has a
**phase** (proposal → planned → landed, or overtaken / withdrawn), a **custody** (owner, until) and
**review** gates. verse's git-issue source reflects the tracker, and a charter would run the phase
machine over it: reminders, review gates, checks on a landing.

A phase needs one value per issue, carried by every client in the field, and settled on a race
rather than by whoever merged last. Two designs were weighed in the discussion (2026-09-22) and
dropped:

- **A phase ref**, `refs/git-issues/v1/phase/<phase>/<xidr>` pointing at the move commit. It keeps
  v1 readers complete, but needs a new tracking prefix, lease and conflict handling in sync, and
  a rule forbidding a second phase ref.
- **A `phase` field in meta.tony.** Every client up to go-tony v0.0.232 parses meta.tony
  non-strictly and rebuilds it from the struct on each edit (`Update`) and each merge
  (`mergeMeta`), so an unknown field is silently erased by the next edit an old client makes.

Labels are a field every client knows and carries, so they reach existing repositories as they
are. What stands in the way is the merge.

## The defect

`mergeList` (`issuelib/merge.go`) merges labels, and every other list, as the union of the two
sides, without the base. A removal on one side is undone by any edit on the other: clone A runs
`unlabel x` while clone B adds a comment, and the merge puts `x` back. For plain labels this
loses an `unlabel`; for a phase it would give an issue two phases after a race that never
happened.

## What is needed

1. **The `git-issue-` label prefix is reserved** for conventions git-issue or a driving program
   defines. Documented where labels are (README, `git issue label` help).
2. **`key=value` labels.** A label containing `=` splits at the first `=` into a key and a
   value, and a key holds one value. `git issue label k=v` on an issue that has `k=u` replaces
   it. Keys and values are lowercased and trimmed like every label, so no convention may depend on
   case. No existing label here contains `=`.
3. **A three-way merge of labels.** The merge compares each side with the base:
   - plain labels are a set: an addition or a removal made on either side survives;
   - a `key=value` key changed on one side takes that side's value, and removed on one side is
     removed;
   - a key changed on both sides to different values is a **merge conflict**: pull refuses that
     issue as it refuses any merge it cannot make, writes nothing, and says which key. Changed on
     both sides to the same value is not a conflict.

   Resolving a conflict is what it is today: set this side to the other's value and pull again, or
   `pull --force` to take the remote's tip. Never last writer wins.
4. **Tests** for each merge case above, including a removal racing an unrelated edit, and a
   phase-shaped key (`git-issue-phase=planned` → `landed` on one side, a comment on the other).

## Old clients

A merge made by go-tony ≤ v0.0.232 is still a union, so it can leave two values for one key: a
stale value brought back, or a real race nobody was stopped on. New clients never write that
shape, so nothing repairs it automatically. On read, a key with more than one value takes the
last one in the list (a later value overwrites an earlier one) and git-issue warns on stderr,
naming the issue, the key and every value. The user fixes it with `git issue label <id> k=v`,
which by (2) leaves the one value.

## Deliberately out of scope

- **The phase machine**: which phases exist, which transitions are allowed, what is terminal, and
  any tombstone convention. That belongs to the program driving it (verse), under
  `git-issue-phase=<phase>` or whatever key it takes from the reserved prefix.
- **Phase and status are orthogonal.** A phase never implies open or closed; a driver that wants a
  terminal phase to close the issue does both.
- **Custody** (owner, until): its own keys, same mechanism.
- **Review** is a gate over a transition. The automation poses it; git-issue need not model it.
