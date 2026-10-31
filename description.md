# stream: the wire drops a head comment just before a closing bracket

A comment on its own line just before a `}` or `]` is lost by the wire decoder. The parser keeps it.

| input | parser (ParseComments) | wire decoder |
|---|---|---|
| `{a: null\n  # h\n}` | `{a: null}` then `# h` | `{a: null}` |
| `{a: !t null\n  # h\n}` | `{a: !t null}` then `# h` | `{a: !t null}` |
| `{a: !t\n  # h\n}` | `{a: !t null}` then `# h` | `{a: !t null}` |

Measured 2026-10-31 on branch issue-v5dn9vfbh12krj after fe9d7a5c, encoding with EncodeComments(true). The first two rows are older than that branch.

The tokenizer makes the comment a head comment (TComment), the kind that leads the value after it. The decoder emits a head-comment event, and nothing follows before the close for it to lead. Where the event is lost hasn't been traced; ReadDocument and EventsToNode are the likely places.

The parser keeps the comment but moves it after the object. Whether a trailing comment inside a container should stay inside it is a separate question. The defect here is the wire losing it, so text a client sends with such a comment reaches a logd or docd store without it.