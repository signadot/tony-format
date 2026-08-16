package main

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// get and list ask their question in kpath, which is the path syntax the rest of
// the system uses: what logd indexes by, what a watch names, what a patch is
// rooted at, and what an error already prints when it says where it is.
//
// They used to ask it in objpath, the older JSONPath-ish one, whose paths begin
// with a '$'. That '$' carries no information -- every path has it -- and its only
// effect was to make the natural spelling fail: get prepended one when the caller
// left it off, so `.a` became `$.a` and worked, while a bare `a` became `$a` and
// did not ("expected '.' or '['"). Beyond the sigil the two languages differ in
// what they can NAME, and the difference ran the wrong way: objpath cannot say
// items(jane) or a{3}, which are ordinary in a store that keys arrays.
//
// So the sigil is gone and the syntax is the system's. A leading $ is still
// accepted, because it is in scripts, and because kpath would otherwise read it as
// a field named "$" and answer "nothing found" -- the one wrong answer worse than
// an error.

// queryPath reads a path argument as kpath, accepting the objpath spelling that
// came before it.
func queryPath(arg string) (string, error) {
	if arg == "" {
		return "", fmt.Errorf("invalid query %q", arg)
	}
	kp := arg
	if kp[0] == '$' {
		// $ alone is the root, and $.a is a.
		kp = strings.TrimPrefix(kp[1:], ".")
	}
	// objpath's any-depth segment, which kpath has no spelling for. It was reachable
	// only as `$...x` -- `..` for the descent and `.x` for the field -- worked only
	// in list, and was documented nowhere, so it is not carried over silently.
	if strings.HasPrefix(kp, "..") {
		return "", fmt.Errorf("%q: kpath has no any-depth segment; name the path, or use a wildcard "+
			"(a.*, a[*], a{*})", arg)
	}
	if _, err := kpath.Parse(kp); err != nil {
		return "", fmt.Errorf("%q: %w", arg, err)
	}
	return kp, nil
}
