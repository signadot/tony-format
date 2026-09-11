# libctl: RunController never hands its Handler the mount's LogdSession, so ControllerConfig.LogdAddr configures a session nothing can use

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); confirmed at 546bd53.

RunController (libctl/controller.go:142) passes cfg.LogdAddr to Mount (:153), which
builds a baseline session (mount.go:94-97). The runtime keeps the client (:164-166), but
no Handler method receives it, so ControllerConfig.LogdAddr (:119) configures a session
nothing can use: it never connects, and is closed on exit (mount.go:221). The Handler
doc ("a logd session (obtained via the mount)") and RunController's inline "calls
MountClient.Unmount" promise access that does not exist.

Graceful unmount is out of reach too: cancelling ctx calls client.Close() (:179), and
docd tombstones the mount (probe: entry present, live=false).

What an author does today: build their own LogdSession from their own logd address inside
the Handler, one per scope if scope-aware -- the reference controller does exactly this
(controller_test.go:129, 147-155).

Fix direction is open: the mount's session is baseline-only while routed operations carry
the client's scope, so handing it over may be the wrong answer; dropping LogdAddr and the
promise, and giving RunController a graceful unmount, may be the right one.