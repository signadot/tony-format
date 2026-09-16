# logd: a set read at a past commit keys arrays by today's schema, so (*) and [*] answer for an array as it is now, not as it was read

setChildren asks identityAt, which reads SchemaFor -- the schema in force NOW. A read at a commit raises keyed arrays by the schema in force at THAT commit (Storage.RaiseState, schemaHistory.ParsedAt). So across a schema change that gave or took an array's identity, a set read at the older commit enumerates `(*)` and `[*]` by the newer schema: `(*)` names nothing on what was then a keyed array, or `[*]` answers positions of what was then an object of names.

Fix: the walk keys arrays by the schema at the commit it reads, as raising does.