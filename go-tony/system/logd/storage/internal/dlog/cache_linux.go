//go:build linux

package dlog

import (
	"os"

	"golang.org/x/sys/unix"
)

// dropPages drops the page cache of [off, off+n) of f; n == 0 means to the end of the
// file. Dirty pages are not dropped, only started on their way to disk.
func dropPages(f *os.File, off, n int64) error {
	return unix.Fadvise(int(f.Fd()), off, n, unix.FADV_DONTNEED)
}
