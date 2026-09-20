package calibration

func DistinctOne(value int) int {
	if value > 0 {
		if value%2 == 0 {
			return value * 2
		}
	}
	return value
}
