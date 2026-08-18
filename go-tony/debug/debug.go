package debug

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type debug struct {
	LoadEnv   bool
	ExpandEnv bool
	Match     bool
	Matches   bool // o build matching logs per document, per match
	Patch     bool // per patch op
	Patches   bool // o build patching logs per document, per patch, not per patch Op
	Op        bool
	Eval      bool
	Read      bool // logd: one line per read -- the path, whether it narrowed, and why not
	Write     bool // logd: one line per commit -- the path, and which phase took the time
}

var d *debug

func init() {
	d = &debug{}
	d.LoadEnv = boolEnv("O_DEBUG_LOAD_ENV")
	d.ExpandEnv = boolEnv("O_DEBUG_EXPAND_ENV")
	d.Match = boolEnv("O_DEBUG_MATCH")
	d.Matches = boolEnv("O_DEBUG_MATCHES")
	d.Patch = boolEnv("O_DEBUG_PATCH")
	d.Patches = boolEnv("O_DEBUG_PATCHES")
	d.Eval = boolEnv("O_DEBUG_EVAL")
	d.Op = boolEnv("O_DEBUG_OP")
	d.Read = boolEnv("O_DEBUG_READ")
	d.Write = boolEnv("O_DEBUG_WRITE")
}

func boolEnv(v string) bool {
	x := os.Getenv(v)
	if x == "" {
		return false
	}
	b, _ := strconv.ParseBool(x)
	return b
}

func LoadEnv() bool {
	return d.LoadEnv
}
func ExpandEnv() bool {
	return d.ExpandEnv
}
func Read() bool {
	return d.Read
}
func Write() bool {
	return d.Write
}
func Match() bool {
	return d.Match
}
func Patch() bool {
	return d.Patch
}
func Matches() bool {
	return d.Matches
}
func Patches() bool {
	return d.Patches
}
func Op() bool {
	return d.Op
}
func Eval() bool {
	return d.Eval
}

func LogAny(v any) {
	d, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", v)
		return
	}
	os.Stderr.Write(d)
}
