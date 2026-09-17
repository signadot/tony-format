# kpath: Join drops the dot before a field that follows a descent and a key or sparse segment

Join(\"..(*)\", \"*\") answers \"..(*)*\", and Join(\"..{3}\", \"*\") answers \"..{3}*\", neither of which parses (\"expected '.', '[', '{' or '(' after a segment, got '*'\"). Without the descent the same joins are right: Join(\"a(*)\", \"*\") is \"a(*).*\". So SplitAll followed by Join does not round-trip \"..(*).*\" or \"..{3}.*\", which is the round trip addsgv1yh12kszdxmdn0 fixed for \"a..[0]\".

Found while building the any-depth read (th7sdhvyh12ksjtfn9n0), which first rebuilt the rest of a pattern from its split segments and hit this; it carries the parsed pattern instead, so logd does not depend on the fix. No stored path holds a descent, so a cursor's Join never meets it.

Probably in how Join decides whether the prefix ends in a segment that a field needs a dot after: a prefix holding `..` is read differently from one without.