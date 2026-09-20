package calibration

func DispatchInteracting(kind, value int) int {
	state := 0
	if kind == 0 {
		state += value
		if value < 0 {
			state = 0
		}
	} else if kind == 1 {
		state += value * 2
		if state > 10 {
			state = 10
		}
	} else {
		state -= value
	}
	return state
}
