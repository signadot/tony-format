# logd: TestSetMatch_PatternAndHistory is flaky -- its reads race the write they are meant to precede

Measured on dc401f31 (before any watch work): 1 failure in 200 runs of `go test ./system/logd/server -run 'TestSetMatch_PatternAndHistory$' -count=200`; seen again in a full package run at 1e39fa7d as `the pattern did not select: [jobs.a1 jobs.a3]` and `trim answered 3 members`.

The test sends reads ("done", "trim") and then a patch ("add" of jobs.a3) on one session through runSet. A read runs OFF the request loop (session_read.go, dispatch) while a patch runs on it, so the patch can commit before the earlier reads take their commit, and they see a3.

Fix belongs in the test: read at an explicit commit (the seed's), or send the write after the reads have been answered.