# logd/libctl: expose CAS (match preconditions) in the session patch protocol

Compare-and-swap is fully implemented at the tx/storage layer but unreachable
from any session client. Wire it through.

## Findings

- CAS exists at the tx layer: api.Patch has Match *PathData, and
  tx/match.go:evaluateMatches runs tony.Match(currentStateAt(match.Path),
  match.Data) for every patcher at commit time, aborting the whole tx if any
  match fails. Works for single-participant (NewTx(1)) and multi-participant
  (all matches evaluated atomically -> cross-key / cross-mount CAS). A failed
  match surfaces as ErrCodeMatchFailed.
- But the session protocol drops it end to end:
  1. api.PatchRequest (the session wire type) has NO match field. api.Patch (which
     has it) is internal to the tx layer and never travels the session wire
     (SessionRequest.Patch is *PatchRequest, not *api.Patch). That internal type
     is likely what was mistaken for "the wire permitted CAS."
  2. handlePatch builds api.Patch{PathData: req.PathData} — Match always nil.
  3. libctl.LogdSession.Patch/PatchTx take no match.
  So no session client (incl. libctl) can do CAS today.

## Plan (additive)

1. Add Match *PathData to api.PatchRequest; regenerate api_gen.go.
2. handlePatch: NewPatcher(&api.Patch{Match: req.Match, PathData: req.PathData}).
3. libctl: PatchIf / PatchTxIf taking a match precondition (bare Patch/PatchTx
   unchanged); surface ErrCodeMatchFailed as a sentinel (ErrMatchFailed). Match
   path is independent of the patch path (as the wire allows).
4. docd: client face already forwards PatchRequest verbatim (base-path CAS routes
   to logd for free). Thread Match through the controller Handler via PatchParams
   (as with TxID) so controllers can do CAS on mounted subtrees.
5. Tests: single-participant CAS pass + fail (ErrMatchFailed); create-if-absent
   (match vs null state) to pin tony.Match semantics; CAS routed through docd to a
   controller.

## Relation

Enabler for docd-coordinated patch splitting (qf58mz22h12krzk2bnn0), which needs
per-participant preconditions preserved. Part of the docd umbrella
(wcabztj2h12ksb9qbnn0).