package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// A transaction's timeout is fixed when it is created: its own if its creator named
// one, else the store's, else tx.DefaultTimeout. It is never none, which is what let a
// transaction short of a participant hold the others, and their sessions, forever
// (hqhyyat8h12ksarmcdn0).
func TestNewTx_TimeoutIsAlwaysBounded(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	timeoutOf := func(named time.Duration) time.Duration {
		t.Helper()
		txn, err := s.NewTxWithTimeout(2, nil, named)
		if err != nil {
			t.Fatalf("NewTxWithTimeout(%v): %v", named, err)
		}
		return txn.Timeout()
	}

	// A store which was given none.
	if got := timeoutOf(0); got != tx.DefaultTimeout {
		t.Errorf("no timeout anywhere: got %v, want tx.DefaultTimeout (%v)", got, tx.DefaultTimeout)
	}
	if txn, err := s.NewTx(2, nil); err != nil {
		t.Fatalf("NewTx: %v", err)
	} else if got := txn.Timeout(); got != tx.DefaultTimeout {
		t.Errorf("NewTx on a store with none: got %v, want tx.DefaultTimeout (%v)", got, tx.DefaultTimeout)
	}

	// Zero given to the store is the default too, not "none".
	s.SetTxTimeout(0)
	if got := timeoutOf(0); got != tx.DefaultTimeout {
		t.Errorf("store set to 0: got %v, want tx.DefaultTimeout (%v)", got, tx.DefaultTimeout)
	}

	// The store's, when the creator names none; the creator's when it does, longer or
	// shorter than the store's.
	s.SetTxTimeout(time.Second)
	if got := timeoutOf(0); got != time.Second {
		t.Errorf("store's: got %v, want 1s", got)
	}
	if got := timeoutOf(time.Hour); got != time.Hour {
		t.Errorf("creator's, longer: got %v, want 1h", got)
	}
	if got := timeoutOf(50 * time.Millisecond); got != 50*time.Millisecond {
		t.Errorf("creator's, shorter: got %v, want 50ms", got)
	}
}
