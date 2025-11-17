package server

import "errors"

// ErrTransactionCompleted indicates the transaction was already completed.
var ErrTransactionCompleted = errors.New("transaction already completed")
