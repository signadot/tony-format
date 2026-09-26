# git issue serve -watch: pull the remote and reload open pages as issues change

`git issue serve` reads this clone's refs when a page is loaded. It pulls nothing, and an open page does not change until it is reloaded, so a person following issues in a browser sees a teammate's pushed comment only after someone pulls and they reload.

## What

`git issue serve -watch` runs the watch `git issue watch` runs, behind the web view:

- it pulls origin every `-fetch` when there is an origin (`--remote` names another, `--local` pulls nothing), and looks at this clone's refs every `-poll`;
- an open issue page reloads when its issue changes, and the index when any does;
- what a pull left standing -- an issue refused, a remote not reached -- is shown on the pages, and goes when it clears.

The view stays read-only: nothing a browser sends changes an issue. The pull writes this clone's refs as `git issue pull` does.

## Also

The comment at the top of commands/serve.go gives sync as the reason serve is read-only: "Sync is force-refspecs in both directions ... nothing in this codebase merges issue refs". Pull has merged, and refused what it cannot merge, since the sync rework. The comment is to say what is true.