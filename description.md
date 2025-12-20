# allow commenting on issues from stdin

⏺ I'll add a worktree workflow guide to the agent coordination issue so it persists for future sessions.

⏺ Bash(./git-issue comment 000017 << 'EOF'
      ## Worktree Workflow for Issues…)
  ⎿  Tool use rejected with user message: please explain the CWD at the beginning instead of the end

⏺ Bash(./git-issue comment 000017 << 'EOF'
      ## Worktree Workflow for Issues…)
  ⎿  Error: Exit code 1

     thread 'main' panicked at /Users/brew/Library/Caches/Homebrew/cargo_cache/registry/src/index.crates.io-1949cf8c6b5b557f/cr
     ossterm-0.28.1/src/event/read.rs:39:30:
     reader source not set
     note: run with `RUST_BACKTRACE=1` environment variable to display a backtrace
     editor failed: editor exited with error: exit status 101