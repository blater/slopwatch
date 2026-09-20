package calibration

type CosmeticAfter struct{ total, updates int }

func acceptCosmetic(delta int) bool {
	if delta < 0 {
		return false
	}
	return true
}
func setCosmetic(c *CosmeticAfter, delta int) {
	c.total += delta
	if c.total > 100 {
		c.total = 100
	}
}
func markCosmetic(c *CosmeticAfter) { c.updates++ }

func (c *CosmeticAfter) Advance(delta int) int {
	if !acceptCosmetic(delta) {
		return c.total
	}
	setCosmetic(c, delta)
	markCosmetic(c)
	return c.total
}
