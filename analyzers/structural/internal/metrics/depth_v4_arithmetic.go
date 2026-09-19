package metrics

import (
	"fmt"
	"math"
	"slopslap.dev/structural/internal/facts"
)

func burdenB8(b facts.Burden) (uint64, error) {
	terms := []struct {
		n      int64
		factor uint64
	}{{b.O, 8}, {b.T, 4}, {b.A, 2}, {b.E, 1}, {b.P, 8}, {b.S, 8}, {b.L, 16}}
	total := uint64(0)
	for _, term := range terms {
		if term.n < 0 || (term.n > 0 && uint64(term.n) > math.MaxUint64/term.factor) {
			return 0, fmt.Errorf("burden overflow")
		}
		product := uint64(term.n) * term.factor
		if math.MaxUint64-total < product {
			return 0, fmt.Errorf("burden overflow")
		}
		total += product
	}
	return total, nil
}
func shallowInteger(b8, h uint64) (*int, error) {
	if b8 == 0 {
		return nil, fmt.Errorf("zero applicable burden")
	}
	if h > (math.MaxUint64-b8)/16 {
		return nil, fmt.Errorf("score denominator overflow")
	}
	denominator := b8 + 16*h
	if b8 > (math.MaxUint64-denominator)/200 || denominator > math.MaxUint64/2 {
		return nil, fmt.Errorf("score arithmetic overflow")
	}
	numerator := 200*b8 + denominator
	value := numerator / (2 * denominator)
	if value > 100 {
		value = 100
	}
	result := int(value)
	return &result, nil
}
