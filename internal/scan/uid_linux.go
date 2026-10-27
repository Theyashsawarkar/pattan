package scan

import (
	"io/fs"
	"syscall"
)

func statUID(info fs.FileInfo) (int, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), true
	}
	return 0, false
}
