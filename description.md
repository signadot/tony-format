# stream: a value or a tag where an object's key goes is refused, as the parser refuses it

The wire decoder's State accepts a value inside an object when no key is waiting for one, and the path keeps the previous key, so the value lands under that key a second time:

- `{a: 1 true}` decodes as `{a: 1 a: true}` (predates c550820c)
- `{a: 1, !delete}` and `{a: 1 !delete}` decode as `{a: 1 a: !delete null}`, which applied as a patch deletes `a`. Before c550820c/2d6db94a these read as `{a: 1}`.
- `{!t}` decodes as `{"": !t null}`
- after a key-set key: `{a !t !u}`, `{a, !t}`, `{a !t {b: 1}}` read `a` twice

The parser refuses all of them.

A tag before a key goes on the key event and is dropped: `{!t a: 1}` decodes as `{a: 1}` and `{a # c\n !t b}` as `{a: null b: null}`. The parser refuses both with "key cannot be tagged".

Fix:
- State.ProcessEvent refuses a value or an open bracket in an object or sparse array with no key waiting.
- The decoder refuses a key with a tag pending.

Found in review of branch issue-v5dn9vfbh12krj.