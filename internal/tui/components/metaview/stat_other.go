//go:build !linux

package metaview

import (
	"io/fs"
	"time"
)

// statExtra has no portable implementation off linux (the Stat_t field
// names differ per GOOS); the overlay renders Modified only there.
func statExtra(fs.FileInfo) (uid, gid uint32, atime, ctime time.Time, ok bool) {
	return 0, 0, time.Time{}, time.Time{}, false
}
