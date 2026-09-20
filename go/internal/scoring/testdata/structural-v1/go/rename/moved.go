package calibration

func RelocatedRoutine(value int) int {
	if value < 0 {
		return -value
	}
	return value * 2
}
