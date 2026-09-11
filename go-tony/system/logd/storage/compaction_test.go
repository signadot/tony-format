package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A schema commit is kept by compaction, and every root snapshot records the schema in
// force at its commit; so however compaction thins the log, a rebuild from it -- the
// manifest gone -- finds the schema and the commit that set it (schema.go,
// index/schema_history.go). The pinned snapshot this replaces kept one snapshot for the
// schema's sake, and a schema snapshot compaction dropped as superseded was the record.
func TestCompact_KeepsTheSchemaAndWhenItWasSet(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustCommit(t, s, nil, `{data: initial}`)
	first := migrateTo(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{data: more}`)
	second := migrateTo(t, s, `{define: {items: {sku: !logd-key null}, tags: {id: !logd-key null}}}`)
	mustCommit(t, s, nil, `{data: most}`)

	// Twice, so the log the schema commits are in is the one compaction rewrites.
	for range 2 {
		if err := s.SwitchDLog(); err != nil {
			t.Fatalf("SwitchDLog: %v", err)
		}
	}
	if err := s.Compact(&CompactionConfig{
		Cutoff:       time.Millisecond, // everything is beyond the cutoff
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  100 * time.Millisecond,
	}); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	check := func(when string, s *Storage) {
		t.Helper()
		if schema, at := s.GetActiveSchema(); schema == nil || at != second {
			t.Errorf("%s: active schema %v at %d, want the second at %d", when, schema != nil, at, second)
		}
		if _, at := s.SchemaAt(first); at != first {
			t.Errorf("%s: the schema at %d was set at %d, want %d", when, first, at, first)
		}
		if got := len(s.schemaForScope(nil).KeyedPaths()); got != 2 {
			t.Errorf("%s: %d keyed paths, want 2", when, got)
		}
	}
	check("after compaction", s)
	re := reopenRebuilt(t, s, root)
	defer re.Close()
	check("rebuilt from the compacted log", re)
}

// TestCompact_SustainedWriteDeleteLoad tests compaction under sustained write/delete load.
// Writes and deletes data until 2 compactions occur, then verifies reads work correctly.
func TestCompact_SustainedWriteDeleteLoad(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Configure compaction with longer intervals to avoid aggressive removal
	// Use 1 hour cutoff to keep all patches, focus on testing the compaction mechanics
	compactionConfig := &CompactionConfig{
		Cutoff:       time.Hour,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  50 * time.Millisecond,
	}
	s.SetCompactionConfig(compactionConfig)

	// Track data we've written for verification
	type record struct {
		key   string
		value int
		alive bool // false if deleted
	}
	records := make(map[string]*record)
	nextValue := 1

	// Helper to write a record
	writeRecord := func(key string, value int) error {
		patch, _ := parse.Parse([]byte(fmt.Sprintf(`{records: {%s: {value: %d}}}`, key, value)))
		tx, err := s.NewTx(1, nil)
		if err != nil {
			return err
		}
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
		if err != nil {
			return err
		}
		result := p.Commit()
		if !result.Committed {
			return fmt.Errorf("commit failed: %v", result.Error)
		}
		return nil
	}

	// Helper to delete a record (set to null)
	deleteRecord := func(key string) error {
		patch, _ := parse.Parse([]byte(fmt.Sprintf(`{records: {%s: null}}`, key)))
		tx, err := s.NewTx(1, nil)
		if err != nil {
			return err
		}
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
		if err != nil {
			return err
		}
		result := p.Commit()
		if !result.Committed {
			return fmt.Errorf("commit failed: %v", result.Error)
		}
		return nil
	}

	// Write initial batch of records
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		if err := writeRecord(key, nextValue); err != nil {
			t.Fatalf("initial write failed: %v", err)
		}
		records[key] = &record{key: key, value: nextValue, alive: true}
		nextValue++
	}

	compactionCount := 0

	// Perform writes, updates, and deletes across multiple log switches
	for round := 1; round <= 4; round++ {
		// Write some new records
		for i := 0; i < 3; i++ {
			key := fmt.Sprintf("round%d_key%d", round, i)
			if err := writeRecord(key, nextValue); err != nil {
				t.Fatalf("write failed at round %d: %v", round, err)
			}
			records[key] = &record{key: key, value: nextValue, alive: true}
			nextValue++
		}

		// Update one existing record
		for key, rec := range records {
			if rec.alive {
				if err := writeRecord(key, nextValue); err != nil {
					t.Fatalf("update failed: %v", err)
				}
				rec.value = nextValue
				nextValue++
				break
			}
		}

		// Delete one record
		for key, rec := range records {
			if rec.alive {
				if err := deleteRecord(key); err != nil {
					t.Fatalf("delete failed: %v", err)
				}
				rec.alive = false
				break
			}
		}

		// Switch logs to trigger compaction
		if err := s.SwitchDLog(); err != nil {
			t.Fatalf("SwitchDLog failed at round %d: %v", round, err)
		}
		compactionCount++
		t.Logf("compaction %d completed at round %d", compactionCount, round)

		if compactionCount >= 2 {
			break
		}
	}

	if compactionCount < 2 {
		t.Fatalf("only %d compactions occurred", compactionCount)
	}

	t.Logf("completed %d compactions with %d records tracked", compactionCount, len(records))

	// Verify reads work correctly after compactions
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit failed: %v", err)
	}

	state, err := readStateAt(s, "", commit, nil)
	if err != nil {
		t.Fatalf("ReadStateAt failed after compactions: %v", err)
	}

	if state == nil {
		t.Fatal("state is nil after compactions")
	}

	// Verify alive records are readable
	aliveCount := 0
	deletedCount := 0
	for _, rec := range records {
		if rec.alive {
			aliveCount++
		} else {
			deletedCount++
		}
	}

	t.Logf("verified state: %d alive records, %d deleted records", aliveCount, deletedCount)

	// Verify we can access the records field
	recordsNode := getField(state, "records")
	if recordsNode == nil {
		t.Fatal("records field not found in state")
	}

	// Count non-null records in state (deleted records have null values)
	accessibleRecords := 0
	nullRecords := 0
	if recordsNode.Fields != nil {
		for i, val := range recordsNode.Values {
			if val != nil && val.Type != 0 { // non-null
				accessibleRecords++
			} else {
				nullRecords++
				t.Logf("null record: %s", recordsNode.Fields[i].String)
			}
		}
	}

	t.Logf("state contains %d non-null records, %d null records", accessibleRecords, nullRecords)

	// The number of non-null records should match alive records
	if accessibleRecords != aliveCount {
		t.Errorf("expected %d non-null records, got %d", aliveCount, accessibleRecords)
	}

	// The number of null records should match deleted records
	if nullRecords != deletedCount {
		t.Errorf("expected %d null records, got %d", deletedCount, nullRecords)
	}
}
