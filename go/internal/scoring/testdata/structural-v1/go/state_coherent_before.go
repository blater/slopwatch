package calibration

type CoherentBefore struct{ total int }

func (c *CoherentBefore) Advance(delta int) int {
	if delta < 0 {
		return c.total
	}
	c.total += delta
	if c.total > 100 {
		c.total = 100
	}
	return c.total
}

func (c *CoherentBefore) AdvanceAgain(delta int) int {
	if delta < 0 {
		return c.total
	}
	c.total += delta
	if c.total > 100 {
		c.total = 100
	}
	return c.total
}
