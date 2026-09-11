// Package tx provides transaction coordination for multi-participant updates.
//
// Enables multiple clients to patch different paths in a document atomically.
// Patches from all participants are merged before commit.
//
// # Usage
//
//	// Create a transaction for n participants; the others find it by its ID
//	// (Storage.GetTx).
//	t, err := store.NewTx(n, scope)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant joins with its own patch.
//	patcher, err := t.NewPatcher(&api.Patch{PathData: api.PathData{Path: "a.b", Data: data}})
//	if err != nil {
//	    // refused: every participant has joined, or the patch cannot be stored
//	}
//
//	// Every participant calls Commit. It blocks until all n have joined; one of them
//	// performs the commit, and each receives the same outcome.
//	result := patcher.Commit()
//	if result.Error != nil {
//	    // handle error
//	}
//	if !result.Matched {
//	    // a precondition did not hold, and nothing was written
//	}
//
// # Commit
//
// The commit runs under the store's commit lock ([CommitOps.LockCommit]). Each
// precondition is lowered ([LowerMatches]) and matched against the state at the current
// commit; positional writes are held to that state ([CheckArrayWritesAt]); the commit
// number is allocated; auto-IDs are injected ([InjectAutoIDs]) and keyed arrays lowered
// to the form the store keeps ([LowerKeyed]); the patches are merged into one patch at
// the root ([MergePatches]); and [CommitOps.WriteAndIndex] stores, indexes and publishes
// it.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
//   - github.com/signadot/tony-format/go-tony/mergeop - Patch operations
package tx
