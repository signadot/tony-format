package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A duration in a config file is written the way a duration is written.
//
// time.Duration is an int64 and implements no text encoding of its own, so a field
// declared as one was read as a NUMBER -- of nanoseconds. An hour was
// `cutoff: 3600000000000`, and `cutoff: 1h` was refused as "expected number, got
// String": the spelling nobody writes was the only one accepted, in a file nobody
// reads back.
func TestConfigDurationsAreWrittenAsDurations(t *testing.T) {
	cfg := writeAndLoad(t, `
compaction:
  cutoff: 24h
  baseInterval: 90m
  slotsPerTier: 4
  multiplier: 4
  gracePeriod: 500ms
tx:
  timeout: 2s
`)
	for _, tc := range []struct {
		name string
		got  Duration
		want time.Duration
	}{
		{"cutoff", cfg.Compaction.Cutoff, 24 * time.Hour},
		{"baseInterval", cfg.Compaction.BaseInterval, 90 * time.Minute},
		{"gracePeriod", cfg.Compaction.GracePeriod, 500 * time.Millisecond},
		{"tx timeout", cfg.Tx.Timeout, 2 * time.Second},
	} {
		if time.Duration(tc.got) != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, time.Duration(tc.got), tc.want)
		}
	}
	// the fields that are not durations are still numbers
	if cfg.Compaction.SlotsPerTier != 4 || cfg.Compaction.Multiplier != 4 {
		t.Errorf("slotsPerTier=%d multiplier=%d, want 4 and 4",
			cfg.Compaction.SlotsPerTier, cfg.Compaction.Multiplier)
	}
}

// A bare number is refused rather than read as nanoseconds. The old spelling has to
// fail loudly: read as a duration, 3600000000000 nanoseconds is an hour and
// 3600000000000 seconds is a hundred thousand years, and silently choosing between
// them is the kind of wrong nobody finds.
func TestConfigDurationRefusesABareNumber(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"the old nanosecond spelling", "compaction:\n  cutoff: 3600000000000\n", "expected string"},
		{"a number with no unit", "compaction:\n  cutoff: 3600\n", "expected string"},
		{"a duration it cannot read", "compaction:\n  cutoff: \"1 hour\"\n", "unknown unit"},
		{"an empty string", "compaction:\n  cutoff: \"\"\n", "invalid duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "logd.tony")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("no error; want one saying %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}

// What is read is what is written: the config a store answers with can be fed back
// to it.
func TestConfigDurationRoundTrips(t *testing.T) {
	cfg := writeAndLoad(t, "compaction:\n  cutoff: 24h\n  gracePeriod: 30s\n")

	out, err := cfg.Compaction.ToTony()
	if err != nil {
		t.Fatalf("ToTony: %v", err)
	}
	if !strings.Contains(string(out), "cutoff: 24h0m0s") {
		t.Errorf("written as:\n%s\nwant a cutoff of 24h0m0s", out)
	}

	again := writeAndLoad(t, "compaction: "+strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[1]))
	_ = again
	var back CompactionConfig
	if err := back.FromTony(out); err != nil {
		t.Fatalf("FromTony on what ToTony wrote: %v", err)
	}
	if back.Cutoff != cfg.Compaction.Cutoff || back.GracePeriod != cfg.Compaction.GracePeriod {
		t.Errorf("round trip: got cutoff=%v grace=%v, want %v and %v",
			time.Duration(back.Cutoff), time.Duration(back.GracePeriod),
			time.Duration(cfg.Compaction.Cutoff), time.Duration(cfg.Compaction.GracePeriod))
	}
}

func writeAndLoad(t *testing.T, body string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logd.tony")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}
