# logd: a single-node read drops its retspec when it builds the node -- on any store with a keyed array, and under any pattern

handleMatch honors `return` on two of its three paths: the existence question (no body, no pattern) and the encoded fast path. The third -- readValueAt, taken when there is a pattern or when the schema keys any array (s.raises()) -- answers NewMatchResponse(id, commit, state) and drops what the spec asked for.

Measured: on a store whose schema keys `runs`, `{match: {path: leaf, return: "path,body"}}` answers the body with no path; `{match: {path: leaf, data: {a: 1}, return: path}}` answers the body it was asked to leave off.

Fix: the built-node path answers what the spec asks, as the other two do.