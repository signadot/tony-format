package storage

// Reading, and which read answers which question.
//
// There is one read of state, Read (cursor.go), and one read of change, Deltas
// (deltas.go). Both are at a path, both are cursors, and the root is a path. What used to
// vary between nine entry points -- whole document or subtree, replayed from a snapshot or
// stepped from a kept document -- is not the caller's to choose: the extent is the path,
// and where to seek from is the store's, answered from the index. A caller reached for one
// and paid for another three times in a month when the axes existed
// (ap8ddvp2h12krd43gdn0, kds4sx3bh12krdrkghn0, ntadpaech12krandgsn0); they do not.
//
// The head is a number. The current commit is the tick's watermark, and every question
// about current state -- a precondition, a write's verification -- is Read at the path it
// concerns, bounded by the write budget. There is no kept document to step, so there is
// no second computation of the state to drift from the first, and nothing to check it
// against (rkb7p8v5h12ksdnmgsn0).
