//go:build linux

package metaview

import (
	"io/fs"
	"syscall"
	"time"
)

// statExtra extracts the owner ids and access/change times from the
// platform stat structure. ok is false when the FileInfo does not carry a
// *syscall.Stat_t; non-linux builds always fall back (see stat_other.go)
// and render Modified only.
func statExtra(info fs.FileInfo) (uid, gid uint32, atime, ctime time.Time, ok bool) {
	st, k := info.Sys().(*syscall.Stat_t)
	if !k {
		return 0, 0, time.Time{}, time.Time{}, false
	}
	// Timespec.Unix() is portable: Sec/Nsec are int32 on linux/arm and
	// linux/386, so the fields cannot be passed to time.Unix directly.
	return st.Uid, st.Gid,
		time.Unix(st.Atim.Unix()),
		time.Unix(st.Ctim.Unix()),
		true
}
