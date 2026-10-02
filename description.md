# docd: a composed watch whose sub-watch ends during setup is ended without ever being confirmed

Found while fixing er3dnqpk.

## Today

`startComposedWatch` (docd/server/watch.go) starts its sub-watches, registers the composed watch, reads the composed state, and only then confirms the watch to the client. A sub-watch that ends in that window ends the composed watch from its own goroutine (`forward`, on `ev.Ended`): the terminal event goes to the client and the watch's entry is removed.

Then one of two things happens:

- The end arrives **before the composed watch is registered**. `startComposedWatch` finds its entry gone, stops the sub-watches and returns. No confirmation is ever sent. The client holds a terminal event for a watch it was never told exists, and libctl's `Watch()` waits for the confirmation forever.
- The end arrives **after registration and before the confirmation**. The client gets the terminal event, then the confirmation, and a state event with it, for a watch that has no sub-watches left.

## When it happens

logd ends a watch right after confirming it when the cursor cannot be replayed: `replay_compacted`, from `forwardEvents`. So a composed watch with an **absolute `fromCommit` below the replay floor** meets this.

**Observed:** the first case, with a `noInit` cursor whose state logd would not replay from (er3dnqpk, before docd was made to end that watch itself). The client's `Watch()` hung.

**Not run:** the cursor below the floor. It sends the same sequence from logd, a confirmation and then an end, through the same code.

er3dnqpk ends the `noInit` cases in docd before any sub-watch starts, so they no longer reach this. A cursor below the floor from a client that takes a state still can.

## What would close it

An end that arrives during setup would be held until the composed watch has been confirmed, and delivered after it, the way a sub-watch's other events are buffered until the composed watch begins. Or docd would refuse a cursor below the floor itself, before starting anything, as it does one past the head; that needs the replay floor, and the ping's `floor` is now where a watch starts as asked, which can be above it.