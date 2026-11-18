package components

import "github.com/egoist/mygo/ui"

// SplitShare converts minimum pane sizes to bounds for a native split ratio.
func SplitShare(preferred, available, firstMin, secondMin float32) float32 {
	available = max(available-1, 1)
	if available < firstMin+secondMin {
		return 100 * firstMin / (firstMin + secondMin)
	}
	return min(max(preferred, 100*firstMin/available), 100-100*secondMin/available)
}

// AdaptiveSplit keeps both panes present and preserves each orientation's drag ratio.
func AdaptiveSplit(c *ui.Context, stacked bool, preferred *float32, size, firstMin, secondMin, reset float32, first, second func()) {
	ratio := SplitShare(*preferred, size, firstMin, secondMin)
	shown := ratio
	split := ui.SplitPercent
	if stacked {
		split = ui.SplitVerticalPercent
	}
	divider := split(c, &ratio, first, second).Grow(1).FillWidth().SplitReset(reset)
	if divider.Changed() && ratio != shown {
		*preferred = SplitShare(ratio, size, firstMin, secondMin)
	}
}
