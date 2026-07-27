# libctl: a writer cannot learn its own commit — the server sends PatchResult{Commit,Data} and doPatch discards it (auto-generated ids unreachable too)

Severity: MEDIUM (missing capability, no data loss). Confidence: read from source at go-tony/v0.0.97 and main.

The server tells a writer which commit its patch landed at, and the client throws it away. Every `LogdSession` write returns `error` and nothing else, so a client cannot learn the commit of its own write — nor read back the data the server returned, which is where auto-generated ids come from.

## Where it is dropped

The server fills both fields, on the ordinary path (system/logd/server/session.go, end of `handlePatch`):

    tx.StripPatchRootTagRecursive(result.Data)
    s.send(api.NewPatchResponse(id, result.Commit, result.Data))

`api.NewPatchResponse` puts them in `SessionResult.Patch`:

    Patch: &PatchResult{
        Commit: commit,   // the commit this landed at
        Data:   data,     // "The patched data (with any auto-generated IDs)"
    }

The client discards the whole result (system/libctl/logd.go, `doPatch`):

    func (s *LogdSession) doPatch(ctx context.Context, req *api.PatchRequest) error {
        resp, err := s.request(ctx, &api.SessionRequest{Patch: req})
        if err != nil {
            return err
        }
        if resp.Error != nil {
            if resp.Error.Code == api.ErrCodeMatchFailed {
                return ErrMatchFailed
            }
            return fmt.Errorf("patch error: %s", resp.Error.Message)
        }
        return nil            // <- resp.Result.Patch, commit and data, never read
    }

`Patch`, `PatchIf`, `PatchTx`, `PatchTxIf` and `PatchWith` all return `doPatch`'s bare error, so there is no spelling that surfaces it. The one place a `PatchResult` is CONSTRUCTED client-side is system/libctl/controller.go:268, which sets `Data` and leaves `Commit` zero — so a controller-served patch does not report a commit even in principle.

Checked at go-tony/v0.0.97 (two tags ahead of the pin this was found on) and on main: unchanged.

## Why it matters

**A writer cannot name what it just did.** Any client that wants to report, log, or hand back "this landed at commit N" has to either watch for its own write to come around on a watch stream and guess which one is its own, or read the current commit afterwards and hope nobody else wrote in between. Downstream (verse), the store's write is the single primitive every other operation is built on, and its result is `(committed bool, err error)` precisely because a commit number would have to be fabricated:

    // There is no commit number in the result because libctl discards it
    // (PatchResult.Commit is dropped by doPatch), and a fabricated one is worse than none.

**Auto-generated ids are unreachable.** `PatchResult.Data` is documented as the patched data *with any auto-generated IDs* — the storage `autoid` machinery's whole output. A client that writes into an auto-id field has no way to learn the id it was given, short of re-reading and inferring.

## Fix direction

Purely client-side; the wire and the server already carry it. Something like:

    // PatchResult is what a write reports back: the commit it landed at, and the data as
    // stored (auto-generated ids filled in).
    type PatchResult struct {
        Commit int64
        Data   *ir.Node
    }

    func (s *LogdSession) doPatch(...) (*PatchResult, error)

with `Patch`/`PatchIf`/`PatchTx`/`PatchTxIf`/`PatchWith` returning it. That is a breaking signature change for every caller; if it should land without one, an additive `PatchWithResult` (or a `*PatchResult` out-param on `PatchOpts`) gets the capability in and lets the plain forms stay.

Worth a test that asserts the returned commit equals the one a watch reports for the same write — the two agreeing is the property, and nothing pins it today.

Files: system/libctl/logd.go (`doPatch` and the five wrappers), system/libctl/controller.go:268 (sets `Data`, leaves `Commit` zero), system/logd/server/session.go (`handlePatch`, already correct), system/logd/api/session.go (`PatchResult`, `NewPatchResponse`).