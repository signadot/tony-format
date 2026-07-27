package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// regWith builds a registry with the given live mount paths.
func regWith(paths ...string) *MountRegistry {
	reg := NewMountRegistry()
	for _, p := range paths {
		_ = reg.Register(&MountEntry{Path: p, Session: &MountSession{}})
	}
	return reg
}

func obj(kv ...any) *ir.Node {
	m := map[string]*ir.Node{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(*ir.Node)
	}
	return ir.FromMap(m)
}

// partsByMount indexes split parts by mount path for assertions.
func partsByMount(parts []mountPart) map[string]*ir.Node {
	m := map[string]*ir.Node{}
	for _, p := range parts {
		m[p.mount.Path] = p.data
	}
	return m
}

func TestSplitPatch_RootSpanningMountsAndBase(t *testing.T) {
	reg := regWith("users", "posts")
	data := obj(
		"users", obj("alice", ir.FromInt(1)),
		"posts", obj("p1", ir.FromInt(2)),
		"config", obj("theme", ir.FromString("dark")),
	)
	parts, base, err := splitPatch(reg, "", data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	byMount := partsByMount(parts)
	if len(byMount) != 2 {
		t.Fatalf("expected 2 mount parts, got %d", len(byMount))
	}
	if got := byMount["users"]; !got.DeepEqual(obj("alice", ir.FromInt(1))) {
		t.Errorf("/users part wrong: %v", got)
	}
	if got := byMount["posts"]; !got.DeepEqual(obj("p1", ir.FromInt(2))) {
		t.Errorf("/posts part wrong: %v", got)
	}
	// Base is written at "config" (not root, which would prefix-conflict with the
	// mount participants).
	if len(base) != 1 || base[0].path != "config" ||
		!base[0].data.DeepEqual(obj("theme", ir.FromString("dark"))) {
		t.Errorf("base remainder wrong: %+v", base)
	}
}

func TestSplitPatch_NestedMountsLongestPrefixWins(t *testing.T) {
	reg := regWith("users", "users.admins")
	data := obj("users", obj(
		"admins", obj("root", ir.FromInt(1)),
		"1", obj("name", ir.FromString("alice")),
	))
	parts, base, err := splitPatch(reg, "", data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	byMount := partsByMount(parts)
	if got := byMount["users.admins"]; !got.DeepEqual(obj("root", ir.FromInt(1))) {
		t.Errorf("/users/admins part wrong: %v", got)
	}
	if got := byMount["users"]; !got.DeepEqual(obj("1", obj("name", ir.FromString("alice")))) {
		t.Errorf("/users part wrong: %v", got)
	}
	if len(base) != 0 {
		t.Errorf("expected no base remainder, got %+v", base)
	}
}

func TestSplitPatch_NonRootPath(t *testing.T) {
	reg := regWith("org.users")
	// patch at "org" writing {users: {...}} -> full tree {org:{users:{...}}}
	data := obj("users", obj("alice", ir.FromInt(1)))
	parts, base, err := splitPatch(reg, "org", data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	if len(parts) != 1 || parts[0].mount.Path != "org.users" {
		t.Fatalf("expected one part for /org/users, got %+v", parts)
	}
	if !parts[0].data.DeepEqual(obj("alice", ir.FromInt(1))) {
		t.Errorf("/org/users part wrong: %v", parts[0].data)
	}
	if len(base) != 0 {
		t.Errorf("expected no base, got %+v", base)
	}
}

func TestSplitPatch_SingleMountNoBase(t *testing.T) {
	reg := regWith("users")
	data := obj("users", obj("alice", ir.FromInt(1)))
	parts, base, err := splitPatch(reg, "", data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(parts) != 1 || len(base) != 0 {
		t.Fatalf("expected single mount part and no base, got parts=%d base=%+v", len(parts), base)
	}
}

func TestSplitPatch_BaseOnly(t *testing.T) {
	reg := regWith("users")
	data := obj("config", obj("theme", ir.FromString("dark")))
	parts, base, err := splitPatch(reg, "", data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("expected no mount parts, got %d", len(parts))
	}
	if len(base) != 1 || base[0].path != "config" ||
		!base[0].data.DeepEqual(obj("theme", ir.FromString("dark"))) {
		t.Errorf("expected base at config, got %+v", base)
	}
}

func TestSplitPatch_RejectsHigherOrderOpAboveMount(t *testing.T) {
	reg := regWith("users", "posts")

	// A merge op (!all) on an object above the mount boundary cannot be attributed
	// to a specific controller -> reject.
	tagged := obj("users", obj("alice", ir.FromInt(1))).WithTag("!all")
	if _, _, err := splitPatch(reg, "", tagged, nil); err == nil {
		t.Error("expected error for higher-order op above mount boundary, got nil")
	}

	// A non-object above the mount boundary likewise cannot be decomposed.
	if _, _, err := splitPatch(reg, "", ir.FromString("scalar"), nil); err == nil {
		t.Error("expected error for non-object spanning mount boundary, got nil")
	}

	// An op WITHIN a single mount's subtree is fine — handed to the controller.
	within := obj("users", obj("list", ir.FromSlice(nil).WithTag("!all")))
	parts, _, err := splitPatch(reg, "", within, nil)
	if err != nil {
		t.Fatalf("op within a mount should be allowed, got: %v", err)
	}
	if len(parts) != 1 || parts[0].mount.Path != "users" {
		t.Fatalf("expected the tagged subtree routed to /users, got %+v", parts)
	}

	// A non-merge tag above the boundary is safe to descend through (default
	// filter lets it pass); the mount is still reached.
	annotated := obj("users", obj("alice", ir.FromInt(1))).WithTag("!custom")
	if _, _, err := splitPatch(reg, "", annotated, nil); err != nil {
		t.Errorf("non-merge tag above boundary should be allowed, got: %v", err)
	}

	// The filter is honored: a permissive filter lets even !all descend.
	allowAll := func(string) bool { return false }
	if _, _, err := splitPatch(reg, "", tagged, allowAll); err != nil {
		t.Errorf("permissive filter should allow !all above boundary, got: %v", err)
	}
}

// echoRoundTrip splits a client patch, has every participant report back the
// data it was handed (what a logd-backed participant reports when there is
// nothing to auto-generate), and rejoins those reports into the client's answer.
func echoRoundTrip(t *testing.T, reg *MountRegistry, path string, data *ir.Node) *ir.Node {
	t.Helper()
	parts, base, err := splitPatch(reg, path, data, nil)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	var reported []participantResult
	for _, p := range parts {
		reported = append(reported, participantResult{path: p.mount.Path, data: p.data})
	}
	for _, b := range base {
		reported = append(reported, participantResult{path: b.path, data: b.data})
	}
	got, err := joinPatchResults(path, reported)
	if err != nil {
		t.Fatalf("join failed: %v", err)
	}
	return got
}

// TestJoinPatchResults_InvertsSplit pins the property that makes a split write
// indistinguishable from an unsplit one: whatever splitPatch takes apart,
// joinPatchResults puts back together at the client's patch path.
func TestJoinPatchResults_InvertsSplit(t *testing.T) {
	for _, tc := range []struct {
		name string
		reg  *MountRegistry
		path string
		data *ir.Node
	}{
		{
			name: "mounts and base",
			reg:  regWith("users", "posts"),
			data: obj(
				"users", obj("alice", ir.FromInt(1)),
				"posts", obj("p1", ir.FromInt(2)),
				"config", obj("theme", ir.FromString("dark")),
			),
		},
		{
			name: "nested mounts",
			reg:  regWith("users", "users.admins"),
			data: obj("users", obj(
				"admins", obj("root", ir.FromInt(1)),
				"1", obj("name", ir.FromString("alice")),
			)),
		},
		{
			// The participant writes at "org.users", above the client's own path.
			name: "non-root client path",
			reg:  regWith("org.users"),
			path: "org",
			data: obj("users", obj("alice", ir.FromInt(1))),
		},
		{
			name: "base only",
			reg:  regWith("users"),
			data: obj("config", obj("theme", ir.FromString("dark"))),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := echoRoundTrip(t, tc.reg, tc.path, tc.data)
			if !got.DeepEqual(tc.data) {
				t.Errorf("join did not invert split:\n got %v\nwant %v", got, tc.data)
			}
		})
	}
}

// TestJoinPatchResults_CarriesGeneratedIDs proves the point of reporting data at
// all: a value the participant filled in — an auto-generated id — reaches the
// client, rather than only what the client sent.
func TestJoinPatchResults_CarriesGeneratedIDs(t *testing.T) {
	stored := joinOrFail(t, "", []participantResult{
		{path: "users", data: obj("alice", obj("id", ir.FromString("u-7")))},
		{path: "config", data: obj("theme", ir.FromString("dark"))},
	})
	want := obj(
		"users", obj("alice", obj("id", ir.FromString("u-7"))),
		"config", obj("theme", ir.FromString("dark")),
	)
	if !stored.DeepEqual(want) {
		t.Errorf("generated id did not survive the join:\n got %v\nwant %v", stored, want)
	}
}

// TestJoinPatchResults_SilentParticipantLeavesHole proves a participant that
// reports no data (a self-backed controller) costs only its own subtree — the
// other participants' data, and their ids, still reach the client.
func TestJoinPatchResults_SilentParticipantLeavesHole(t *testing.T) {
	got := joinOrFail(t, "", []participantResult{
		{path: "users", data: obj("alice", obj("id", ir.FromString("u-7")))},
		{path: "opaque", data: nil},
	})
	want := obj("users", obj("alice", obj("id", ir.FromString("u-7"))))
	if !got.DeepEqual(want) {
		t.Errorf("silent participant should leave a hole, not void the result:\n got %v\nwant %v", got, want)
	}

	// Every participant silent: nothing to report.
	if got := joinOrFail(t, "", []participantResult{{path: "a"}, {path: "b"}}); got != nil {
		t.Errorf("expected no data when no participant reported any, got %v", got)
	}
}

func joinOrFail(t *testing.T, clientPath string, reported []participantResult) *ir.Node {
	t.Helper()
	got, err := joinPatchResults(clientPath, reported)
	if err != nil {
		t.Fatalf("join failed: %v", err)
	}
	return got
}
