# git-issue: a push sends every issue in one connection

git-issue push -all makes one git push --atomic per issue, each its own HTTPS connection. The v0.0.230 migration touched ~350 issues and ran ~12 minutes with no output, looking hung. Batch the refs into one push with a lease per ref, keep what must be atomic together, and show progress.