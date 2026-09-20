package calibration

func Indexed(values []int, queries []int) int {
	index := make(map[int]int, len(values))
	for i, value := range values {
		index[value] = i
	}
	found := 0
	for _, query := range queries {
		found += index[query]
	}
	return found
}
