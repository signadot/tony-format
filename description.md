# add a streaming nav to logd/docd sessions (ls is nav at depth 1)

logd/docd session protocols support an async watch flow which should be
generalised to support out of memory listing of children

roughly (exact syntax is rough here)

=> {watch: { path: ... }}  # send a watch request
<== {watch: { id: ... }}   # watch request accepted, the stream gets an id
<== {watch: { id: ... patch: ...e }}  # an event for the stream is returned, one at a time, async

now we can use the same flow for listing document nodes with large/out of memory fanout

=> {list: { path: ... field|key-only: false|true}}
<== {list: { id: ...}}
<== {list: { id: ... elt: E}} # E is either a string/scalar (object key or !logd-key or !logd-auto-id) or in the form { KEY: <contents>}