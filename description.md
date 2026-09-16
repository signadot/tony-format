# libctl: MatchPaths and MatchIDs over a path that names one node hang until the context ends

matchSetPage takes an answer for the single-node answer only when it carries neither path nor id (`single := m.Path == "" && m.ID == "" && !wildPath(...)`). A path naming one node with `return: path` or `return: id` is answered once, WITH the path or id, so the page loop waits for a done marker that a single-node read never sends.

Measured: MatchPaths(ctx, "jobs.a", nil) with a 3s context delivers [jobs.a] and then returns context deadline exceeded after 3s. MatchEach with the default retspec is unaffected, since its single answer carries neither.

Fix: whether the answer is one node or a set is the path's question (wildPath), not the answer's shape.