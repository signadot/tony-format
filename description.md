# logd: a switch under an in-flight append corrupts a log, and a corrupt record keeps the store from opening

Staging, docd-0, at start:

    WARN log file has a region the frame walk cannot cross; appending past it
         path=/data/logB stoppedAt=84096896 size=84784232 bytesBeyond=687336
    failed to initialize storage: failed to rebuild index: failed to read entry:
      failed to read from logB: failed to deserialize entry: bad literal at
      `...verse\x00\x00\x00\x04\x06...` at offset 5

Two defects, one that wrote the bytes and one that made them fatal.

HOW THE BYTES GOT THERE. DLog.AppendEntry read which log was active under dl.mu and
RELEASED IT before appending:

    dl.mu.Lock(); activeLog := dl.activeLog; dl.mu.Unlock()
    ... resolve the file ...
    logFileObj.AppendEntry(entry)

So a committer could resolve logB, be descheduled, and have SwitchActive flip logB to
inactive underneath it. A snapshot writes into the INACTIVE log on the assumption that
nobody appends there -- SnapshotWriter.Write is a WriteAt at its own tracked position --
so the descheduled append lands inside the snapshot blob. The result is exactly what the
log shows: a region no frame walk can cross, with live bytes beyond it.

The window has always been there. Making the snapshot run off the commit path
(dvgz9308h12ks4xmgdn0, v0.0.167) is what turned "a switch while another session happens
to be mid-append" into the ordinary case. Fixed by holding dl.mu across the append, so a
switch cannot happen between resolving the log and writing to it.

WHY IT WAS FATAL. index.Build returned the deserialization error, storage.Open failed,
and docd never started. But framing past a bad frame cannot be trusted, so what lies
beyond is unreachable however the store reacts -- refusing to open recovers none of it
and keeps the system down. Build now stops the walk at such a record, logs an ERROR
naming the log and offset, and the store opens with what it could read.
Storage.StatsReport carries `log.unreadable` for as long as the process runs, so it is
not something an operator discovers weeks later in a log file.

STILL OPEN, and the reason this issue is not closed: the index loaded from index.gob
still holds segments pointing PAST the unreadable record, so a read which needs one of
them fails ("failed to read patch entry ... unknown event type"). The store opens and
writes, and reads which do not touch those segments work, but the ones that do are
broken. The repair is to drop index segments for that log at or beyond the bad position
-- the readable state is what the store can actually read -- and to say how much was
dropped. TestStoreOpensOverAnUnreadableRecord pins today's honest behaviour and will be
tightened when that lands.

Related: dvgz9308h12ks4xmgdn0 (the async snapshot), ps8kfs9dh12kr777fnn0 (snapshots under
o sys up).