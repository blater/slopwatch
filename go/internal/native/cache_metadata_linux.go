package native

import (
	"os"
	"syscall"
)

func fileChangeTime(info os.FileInfo) int64 {
	stamp := info.Sys().(*syscall.Stat_t).Ctim
	return stamp.Sec*1e9 + stamp.Nsec
}
