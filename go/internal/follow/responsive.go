package follow

func responsiveTier(width, height int) ResponsiveTier {
	if width < 36 || height < 6 {
		return ResponsiveResize
	}
	if width < 60 {
		return ResponsiveCompact
	}
	if width < 96 {
		return ResponsiveMedium
	}
	return ResponsiveFull
}
func fullScreenSurface(width, height int) bool {
	return width < 60 || height < 16
}
func resizeView(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, height)
	for index := range lines {
		lines[index] = padANSI("", width)
	}
	lines[0] = padANSI(truncate("RESIZE TERMINAL", width), width)
	if height > 1 {
		lines[1] = padANSI(truncate("Need at least 36 columns x 6 rows", width), width)
	}
	if height > 2 {
		lines[2] = padANSI(truncate("q quit · resize to continue", width), width)
	}
	return joinScreenLines(lines)
}
