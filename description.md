# logd storage: one process opens a data dir at a time

`storage.Open` (system/logd/storage/storage.go) takes no lock on its root. As of v0.0.249 nothing in go-tony does: no flock or lock file anywhere. Two processes, `o sys up --data D` and `o logd --data D` in any combination, both open D and write one store.

This came up in verse, where a data dir is meant to be one verse with one owner (verse issue xk2pp5j0h12ksbv1wxn0):
- `verse up` brings the store up with `o sys up --data <dir>`. It finds a pair the last run left behind only through a `docd.addr` file that it writes itself.
- A deployment that runs `o sys up` directly (verse PR #63, under pitchfork) writes no such file.
- A `verse up` on the same dir then starts a second logd and docd beside the first.
- A lock taken by verse cannot cover this, because verse is not the process that holds the store.

Tentative fix:
- `storage.Open` takes an exclusive lock on a file in the root, for example `flock` on `<root>/LOCK`, and holds it for as long as the `Storage` is open.
- A second open of the same root is refused at once with an error that names the root, and the holder's pid if recorded.
- A lock held by a dead process is released by the kernel, so a crash leaves nothing to clean up.
- `o sys up` fails before it binds any port.