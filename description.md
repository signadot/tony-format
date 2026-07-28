# store client has no failure state: a dead session, a torn tail, a panic and saturation all surface as List returning [] beside a healthy verse status

The store client has no failure state. A tokenizer panic, saturation under ~27 concurrent
agents, a torn tail, and a dropped docd connection all produce the same surface: `verse
status` reports a healthy rev and triggers, while every `List` returns `[]`.

An empty result is indistinguishable from a broken session. That is the whole bug.


Each underlying fault has its own fix and they keep arriving — this week alone: a tokenizer
panic taking the daemon down, an append path writing at the wrong offset, a snapshot lookup
that never fired, a scan that proposed deleting 179 MB. Every one of them surfaced to the
operator as "no entities". The failures are diverse; the symptom is a constant.

Concretely, it misled its own author today: `[]` across five systems read as "the entities
are gone". They were not — the session had dropped and never recovered. That is the failure
mode to design against, and no amount of fixing individual causes addresses it.


A client that cannot serve a read must say so, not return an empty one. That means:

- A connection state on the store client, and `List`/`Match`/`Watch` returning an error
  rather than a zero value when it is not `connected`.
- `verse status` reporting that state. A healthy rev and trigger list next to a dead session
  is the specific thing that made `[]` believable.
- Distinguishing "queried successfully, found nothing" from "could not query". Today both
  are `[]`.

`data6-imbalanced` is the honourable exception and the model for the rest: it refuses to
start. Loud and deterministic beats silent and plausible.


Individual causes seen producing this surface, for the record rather than as the fix:
`pb1aj0sqh12ksp38cxn0` (tokenizer panic in the read pump), `v67hjrjbh12ksarmcdn0`
(throughput ceiling under concurrent load), `hqhyyat8h12ksarmcdn0` (dispatch/shutdown
blocking). Reported by scott from operating verse.