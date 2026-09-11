// Package debug provides debugging utilities for Tony development.
//
// Each switch -- [LoadEnv], [ExpandEnv], [Match], [Matches], [Patch], [Patches],
// [Op], [Eval], [Read], [Write] -- reports whether its environment variable
// held a true value, as strconv.ParseBool reads one, when the process started.
// The variable is the switch's name in upper snake case after O_DEBUG_:
// O_DEBUG_MATCH for Match, O_DEBUG_LOAD_ENV for LoadEnv.
//
// [Logf] writes to standard error, rendering an *ir.Node argument as Tony and a
// map[string]any, []any or json.Number as indented JSON.
package debug
