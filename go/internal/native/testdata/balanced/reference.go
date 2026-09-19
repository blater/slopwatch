package reference

func Choose(a, b, c, d, e, f bool) int {
	if a {
		if b {
			if c {
				if d {
					if e {
						if f {
							return 1
						}
					}
				}
			}
		}
	}
	return 0
}
