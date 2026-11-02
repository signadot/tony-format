//go:build !linux

package dlog

import "os"

// dropPages does nothing off Linux. See the Linux build.
func dropPages(f *os.File, off, n int64) error {
	return nil
}
