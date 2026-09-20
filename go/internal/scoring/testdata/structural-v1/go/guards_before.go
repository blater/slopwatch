package calibration

func GuardsBefore(a, b bool) bool {
	if a {
		if b {
			return true
		}
	}
	return false
}
