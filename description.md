# libctl: 'dropping response with no matching request' logs the id POINTER, so the warning cannot be correlated

`LogdSession.deliverResponse` logs the request id as `resp.ID`, which is a `*string`:

    // system/libctl/logd.go:567
    s.log.Warn("dropping response with no matching request", "id", resp.ID)

slog formats the pointer, so every line reads:

    WARN dropping response with no matching request component=logd-session id=0x14001bf5570
    WARN dropping response with no matching request component=logd-session id=0x14001bf5640
    WARN dropping response with no matching request component=logd-session id=0x14001bf5710

The id is the one thing that would make the message actionable — it is what ties the dropped
response back to the request that was abandoned — and it is the one thing the line does not
carry. Addresses of freshly allocated strings are also all-distinct, so a burst gives the
false impression that every drop is a different request.

Fix is `*resp.ID` guarded for nil (the branch is reachable with `resp.ID == nil`, which is
itself worth distinguishing — an id-less response is a different fault from an unmatched one):

    id := "<none>"
    if resp.ID != nil {
        id = *resp.ID
    }
    s.log.Warn("dropping response with no matching request", "id", id)

## How it showed up

Downstream (verse), `verse connect` on a 300-file tree produced ~1000 of these in a minute.
The underlying cause is throughput, not a defect here — see v67hjrjbh12ksarmcdn0, to which I
am adding the numbers — but diagnosing it took reading libctl's source, because the log line
itself distinguishes nothing. With real ids it would have been obvious at a glance that the
drops were late answers to a bounded set of timed-out requests rather than a runaway.

Worth noting the branch is *expected* to be reachable: `request()` deletes its pending entry
on ctx cancellation (logd.go:520-522), so any request whose caller timed out will produce
exactly this warning when the server answers. That makes it a normal-operation log line under
load, which raises the bar for it being readable — and is an argument for demoting it to Debug
once it carries an id, since it does not by itself indicate a fault.