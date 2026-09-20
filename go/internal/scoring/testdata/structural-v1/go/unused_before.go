package calibration

func UnusedBefore(x int) int {
	used := x * 2
	if x > 0 {
		unused := x * 3
		_ = unused
	}
	return used + 1
}
