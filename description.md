# logd: a set answers which members there are, not what kind each is -- a client cannot tell what to list under one, or with which wildcard

A match at `jobs.*` with `return: "id"` names the members, and nothing says which of them are containers or how their children are addressed. A client browsing the store has to read each body to find out, which is the read `return` exists to avoid.

The IR type does not answer it: a sparse array is an Object (tagged `!sparsearray`), and a keyed array is an Array to the client and an object of names in the store, keyed only by the schema. So `Object` could mean `.*` or `{*}`, and `Array` could mean `[*]` or `(*)`, and `[*]` on a keyed array names nothing.

Ask: a retspec name `iterType`, answering `Object | SparseArray | Array | KeyedArray | String | Number | Bool | Null`. Each container maps to its wildcard -- `.*`, `{*}`, `[*]`, `(*)` -- so the answer alone says what to list and how. It is read from the first event of the member plus the schema, so no node is built.

Mounts: `return` is not understood past docd today. A composed read (mounts beneath the path) and a controller's match (libctl controllerRuntime.handleMatch) both answer a body whatever the retspec asked. Both refuse a non-default retspec as unsupported. A pass-through to logd is unchanged.