# TestWatch_EveryRepository flakes under load: a fixed 500ms wait for the watch

git-issue/commands/watch_test.go `TestWatch_EveryRepository` comments in two repositories, sleeps 500ms, then asserts what four watches reported. Under the whole suite's load the watches can report later than that, and the test fails with an empty or partial report, e.g.

    watch_test.go:947: the watch from a of an issue of each said:

Seen once during the full git-issue suite while working on 9akz9nkwh12krp72vnn0, whose change does not touch the watch; the test passed 8 times out of 8 alone, and the package passed on a rerun.

Proposal: wait for each watch's expected lines with a deadline (poll the buffer until they appear, failing after some seconds) instead of a fixed sleep, so the test waits as long as the watch needs and no longer.