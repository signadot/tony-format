# logd: a bounded descent lists one level past its bound, and five more findings from the depth review

Review of e6030b4d (zpx2x6b0h12ks05andn0), findings ripe to address, in order:

1. Positions.Live counts a descent that has taken its depth as live, so both walkers list one level past the bound and find nothing there: `jobs..` at depth 1 over ten thousand jobs is ten thousand and one listings, not one. Answers are right; the cost the commit and session.md promise is not delivered.
2. `o list -depth -1` is accepted on any path: the flag's default is the unbounded sentinel, so a typed -1 skips both refusals while -2 is refused and logd refuses -1.
3. Position sets grow in unbounded walks: dedup is on {segment, taken}, and taken means nothing when unbounded, so `..b..c` carries one copy of the second descent per level where b reopens it.
4. docd's composed read and .meta read rebuild the request without depth, so a depth logd would refuse is silently ignored when a mount sits under the path.
5. docd refuses every bounded descent whose `..` precedes the mount's segments, even one too shallow to reach the mount.
6. The depth rule lives at two entry points with two messages and not in kpath, which every entry point calls; ir accepts a depth on a path with no `..` and a negative below -1 acts as 0.

Cleanups: a HasDescend method beside HasWild instead of two private loops; the depth paging helper in the test duplicates the unbounded one and drops two assertions.

Not addressed: old cursors refused after the format grew a field (ephemeral); the cursor not pinning data or return (predates the change).