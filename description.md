# logd: index not persisted on every commit - slow recovery after crash

The index is only saved on Close() and during init(). If the server crashes, index rebuild happens from maxCommit+1 which is correct, but could be slow for large logs.

Consider periodic index checkpointing to reduce recovery time after crashes.