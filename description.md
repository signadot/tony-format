# logd: indexing a patch costs more as the store ages -- 4.5ms of a 4.7ms write on staging

Split out of v552mdbqh12kr7dtgdn0, whose fold half is fixed. This is what is left, and
on staging it is now the whole of a write:

    writes.avg          4.751ms
    writes.avg.index    4.547ms      <- here
    writes.avg.apply       88µs
    writes.avg.append     109µs

Measured locally against accumulated commits, with the entity count held at 500 so only
the commit history grows:

     1000 commits   index avg   397µs
     5000 commits   index avg 1.229ms
    20000 commits   index avg 3.789ms

IndexPatch adds a segment at every path inside a patch, and each goes into that node's
commit tree -- the root's tree gets one per commit, so it holds the whole history of the
store. Whatever the growth is (tree copying on insert, comparator cost, allocation), it
is per write and it does not come back down: staging is at fifty thousand commits.

Note the shape is different from the persist stall (v552mdbqh12kr7dtgdn0, fixed in
v0.0.169): this one is the MEDIAN write getting slower, not an occasional multi-second
one.

Worth measuring before fixing. The counters report it from any running store
(writes.avg.index), and compaction's effect on it is part of the question -- a store
whose old segments are compacted away may not accumulate at all.