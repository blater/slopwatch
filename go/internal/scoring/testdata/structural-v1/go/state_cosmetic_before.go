package calibration

type CosmeticBefore struct{ total, updates int }

func (c *CosmeticBefore) Advance(delta int) int {
	if delta < 0 {
		return c.total
	}
	c.total += delta
	if c.total > 100 {
		c.total = 100
	}
	c.updates++
	return c.total
}
