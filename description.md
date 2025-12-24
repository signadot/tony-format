# logd: transaction timeout/cleanup missing - transactions leak if not all participants join

InMemoryTxStore has no TTL or cleanup mechanism. If a transaction is created but not all participants join, it leaks forever.

If a client calls newtx: { participants: 3 } but only 2 patches are submitted, the transaction hangs indefinitely blocking those 2 participants.

Solution:
- Add TTL to transactions with default value of 1s in config
- Background goroutine to clean up stale transactions
- Participants waiting on incomplete transactions should receive a timeout error