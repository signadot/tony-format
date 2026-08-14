// Package buildinfo answers which build of this module a binary is.
//
// The answer comes from the build itself, runtime/debug.ReadBuildInfo, and not
// from a string stamped in with -ldflags -X. These binaries reach a machine
// through the module proxy -- "go install .../cmd/o@latest" -- which is a build
// path no linker flag can reach, so an injected version would be empty in
// exactly the case a user is most likely to ask about. It would also have to be
// passed by every other path that builds one: the Makefile, a bare "go build
// ./cmd/o", "go run". The toolchain already records this for all of them.
//
// What the toolchain records is a version for a proxy install and a revision
// for a build from a checkout, so those are the two shapes below. Anything more
// -- dependency versions, build flags, the toolchain itself -- is in the binary
// too, and "go version -m <binary>" prints all of it.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version returns the version of the running binary.
//
// A binary installed from the module proxy reports its module version, such as
// "v0.0.129". A binary built from a checkout has no such version -- the
// toolchain calls it "(devel)" -- so it reports the commit it was built from,
// that commit's time, and "dirty" when the tree had uncommitted changes:
//
//	v0.0.129
//	(devel) 7454eff 2026-08-13T20:09:51Z dirty
//
// Released versions are tagged "go-tony/v0.0.129" in the repository, since the
// module lives in a subdirectory. The toolchain reports the version part alone,
// which is the part "go install ...@v0.0.129" takes; the tag needs the prefix
// put back.
func Version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return version(bi)
}

// Line returns the version preceded by the command's name, which is what a
// `version` subcommand prints: "o v0.0.129".
func Line(name string) string {
	return name + " " + Version()
}

// version formats bi. It is separate from Version so a test can pose the build
// shapes -- released, devel, dirty, unstamped -- that one test binary cannot be
// all of.
func version(bi *debug.BuildInfo) string {
	v := bi.Main.Version
	switch v {
	case "":
		// No main module version at all: a binary built outside module mode,
		// or a test binary in some toolchains.
		v = "unknown"
	case "(devel)":
	default:
		return v
	}

	parts := []string{v}
	settings := map[string]string{}
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	if rev := settings["vcs.revision"]; rev != "" {
		parts = append(parts, shortRev(rev))
	}
	if t := settings["vcs.time"]; t != "" {
		parts = append(parts, t)
	}
	if settings["vcs.modified"] == "true" {
		parts = append(parts, "dirty")
	}
	return strings.Join(parts, " ")
}

// shortRev abbreviates a revision to git's customary seven characters, leaving
// anything shorter -- or anything that is not a git hash -- as it is.
func shortRev(rev string) string {
	if len(rev) <= 7 {
		return rev
	}
	return rev[:7]
}
