package calibration

func DistinctTwo(value int) int {
	if value > 0 {
		if value%2 == 0 {
			return value * 2
		}
	}
	return value
}

func AnotherDifficult(value int) int {
	if value < 0 {
		if value%2 != 0 {
			return -value
		}
	}
	return value
}
