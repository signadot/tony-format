# docd: every wildcard match is refused as needing composition, including a set no mount or clock is near

Reported by an agent trying list-paths: `list verse.demo: match error: unsupported: "verse.demo.*" names a set, and docd cannot compose one across mounts yet`, with no mount at, above or under verse.demo.

client_session.go refuses `req.Match != nil && hasWildSegment(path)` before any routing, so a set is unsupported through docd whatever the mount set is. The refusal was meant for a set whose members can live in more than one place (343cd890, composing is 5f6vrzw0h12ksrtfn9n0); a set that can only be logd's is that question's easy case, and docd's logd pump already forwards a set's responses as they come.

Fix: refuse only when the pattern can reach a mount or a clock -- a member at, under or above one, compared segment by segment (a field wildcard matches any field, an index kind no mount field) -- and pass the rest through.