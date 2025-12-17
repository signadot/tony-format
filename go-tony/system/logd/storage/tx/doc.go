// Package tx provides transaction coordination for multi-participant updates.
//
// Enables multiple clients to patch different paths in a document atomically.
// Patches from all participants are merged before commit.
//
// # Usage
//
//	// Create transaction
//	tx := storage.NewTx(participantCount, metadata)
//
//	// Each participant gets a patcher
//	patcher := tx.NewPatcher(patch)
//
//	// Commit and wait for result
//	result := patcher.Commit()
//	if result.Err != nil {
//	    // handle error
//	}
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
//   - github.com/signadot/tony-format/go-tony/mergeop - Patch operations
package tx
