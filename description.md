# logd: MigrationPatch bypasses every write check, and the damage lands at CompleteMigration

Every write is applied to current state before it is stored (8e1b334), because a delta the
store cannot apply is not a failed write but a permanent one. `Storage.MigrationPatch` is not
a write in that sense: it allocates a commit, builds an entry and appends it to the dlog
directly, so it is held to none of the checks the commit path applies.

Skipped, as of 0ad1931:

  - verifyApplies       -- does this delta apply at all
  - checkUnsafeWrite    -- !pipe and anything else that calls out to the system
  - the array-element check for a path naming no element
  - checkStorableInScope (baseline-only path, so not reachable here, but the pattern is the
    same: the vocabulary is enforced where doCommit runs and not here)

## Reproduce

```go
// baseline holds {verse: {task: {instance: null}}}

// the ordinary path refuses it
Patch("verse.task.instance", `{shape: !irtype ""}`)
//  -> the patch does not apply to the current state, so storing it would make every
//     read of the store fail: irtype patching "null" gave cannot patch with irtype operation

// the migration path takes it
StartMigration(schema)
MigrationPatch("verse.task.instance", `{shape: !irtype ""}`)   // commit=4, err=<nil>

ReadStateAt("", commit, nil)   // still fine -- the entry is in the PENDING index only
AbortMigration()               // still fine -- it is never promoted

CompleteMigration()
ReadStateAt("", commit, nil)
//  -> failed to apply patches: irtype patching "null" gave cannot patch with irtype operation
```

`!irtype` is a MATCH operator (mergeop/match.go: "cannot patch with %s operation"), so this is
what a document which CONTAINS operators -- a schema, a rule, a shape declaration -- looks like
when it is stored without the !raw escape. It is one shape of many; an out-of-range arraydiff
or a !pipe would do as well.

## What makes it sharp

The delta is invisible while the migration is in progress, because it is indexed into the
pending index alone. Nothing goes wrong at the write, nothing goes wrong during the migration,
and nothing goes wrong on abort. It breaks at COMPLETION -- the one moment the operator is
least expecting the store to be about the delta they wrote some time ago -- and then it is
permanent, since every read replays it and no later patch can repair a read which dies on the
way past.

Seen in production as

    "msg":"failed to read state for init","path":"verse.task.instance","commit":1203,
    "error":"failed to apply patches: irtype patching \"null\" gave cannot patch with irtype operation"

though that store predates 8e1b334, so this issue is about the door which is still open rather
than about that particular entry.

## Shape of a fix

Route it through the same verification. The one question worth settling first is what state a
migration patch should be verified against: it is applied to baseline after promotion, and the
pending index is the same log filtered, so baseline at the allocated commit looks right -- but a
migration exists to change the SHAPE of what is stored, and a transformation which does not
apply to the state as it stands today may be exactly what is intended. That is worth answering
before the check is written, or the check will refuse the migrations it exists to protect.