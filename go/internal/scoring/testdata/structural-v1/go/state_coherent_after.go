package calibration

type CoherentAfter struct{ total int }

func applyCoherent(c *CoherentAfter, delta int) int {
	if delta < 0 {
		return c.total
	}
	c.total += delta
	if c.total > 100 {
		c.total = 100
	}
	return c.total
}

func (c *CoherentAfter) Advance(delta int) int { return applyCoherent(c, delta) }
func (c *CoherentAfter) AdvanceAgain(delta int) int { return applyCoherent(c, delta) }
