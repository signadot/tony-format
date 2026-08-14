package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func settings(kv ...string) []debug.BuildSetting {
	var out []debug.BuildSetting
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return out
}

// TestVersion_BuildShapes: one test binary is only ever one of these, so pose
// them. The distinction that matters is that a proxy install says a version and
// nothing else, while a build from a checkout has no version to say and must
// name the commit instead -- including whether that commit was what was built.
func TestVersion_BuildShapes(t *testing.T) {
	const rev = "7454eff0f1efbc2b209a27f1b25a888a403cbd34"
	for _, tc := range []struct {
		name string
		bi   debug.BuildInfo
		want string
	}{
		{
			name: "released",
			bi:   debug.BuildInfo{Main: debug.Module{Version: "v0.0.129"}},
			want: "v0.0.129",
		},
		{
			name: "released ignores stale vcs stamps",
			bi: debug.BuildInfo{
				Main:     debug.Module{Version: "v0.0.129"},
				Settings: settings("vcs.revision", rev, "vcs.modified", "true"),
			},
			want: "v0.0.129",
		},
		{
			name: "checkout",
			bi: debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: settings("vcs.revision", rev, "vcs.time", "2026-08-13T20:09:51Z"),
			},
			want: "(devel) 7454eff 2026-08-13T20:09:51Z",
		},
		{
			name: "checkout with uncommitted changes",
			bi: debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: settings("vcs.revision", rev, "vcs.time", "2026-08-13T20:09:51Z", "vcs.modified", "true"),
			},
			want: "(devel) 7454eff 2026-08-13T20:09:51Z dirty",
		},
		{
			name: "no vcs stamp",
			bi:   debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: "(devel)",
		},
		{
			name: "no version at all",
			bi:   debug.BuildInfo{},
			want: "unknown",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := version(&tc.bi); got != tc.want {
				t.Fatalf("version = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVersion_ReadsThisBuild: whatever this test binary is, it has to have an
// answer, and Line has to put the command's name in front of it.
func TestVersion_ReadsThisBuild(t *testing.T) {
	v := Version()
	if v == "" {
		t.Fatal("Version returned the empty string")
	}
	if want := "o " + v; Line("o") != want {
		t.Fatalf("Line = %q, want %q", Line("o"), want)
	}
	if strings.Contains(v, "\n") {
		t.Fatalf("Version spans lines: %q", v)
	}
}
