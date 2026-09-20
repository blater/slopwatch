package calibration

func Rescan(values []int, queries []int) int {
	found := 0
	for _, query := range queries {
		for i, value := range values {
			if value == query {
				found += i
				break
			}
		}
	}
	return found
}
