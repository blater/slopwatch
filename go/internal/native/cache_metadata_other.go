//go:build !darwin && !linux

package native

import "os"

func fileChangeTime(info os.FileInfo) int64 { return info.ModTime().UnixNano() }
