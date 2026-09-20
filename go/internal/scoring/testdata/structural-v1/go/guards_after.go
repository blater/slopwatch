package calibration

func GuardsAfter(a, b bool) bool {
	if !a {
		return false
	}
	if !b {
		return false
	}
	return true
}
