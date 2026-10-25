package styles

import "testing"

func TestWidthTierFor(t *testing.T) {
	cases := []struct {
		width int
		want  WidthTier
	}{
		{-1, TierFull}, // unmeasured → full
		{0, TierFull},  // unmeasured → full
		{40, TierTiny}, // below narrow floor
		{57, TierTiny}, // just below narrow
		{58, TierNarrow},
		{79, TierNarrow},
		{80, TierMedium},
		{99, TierMedium},
		{100, TierFull},
		{200, TierFull},
	}
	for _, c := range cases {
		if got := WidthTierFor(c.width); got != c.want {
			t.Errorf("WidthTierFor(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

func TestDialogWidth(t *testing.T) {
	cases := []struct {
		window int
		want   int
	}{
		{0, dialogMinWidth},   // unmeasured
		{30, 26},              // tiny: window-margin (fits, below floor)
		{60, dialogMinWidth},  // 2/3=40 == floor
		{90, 60},              // 2/3 within range
		{200, dialogMaxWidth}, // capped
	}
	for _, c := range cases {
		got := DialogWidth(c.window)
		if got != c.want {
			t.Errorf("DialogWidth(%d) = %d, want %d", c.window, got, c.want)
		}
		if c.window > 0 && got > c.window {
			t.Errorf("DialogWidth(%d) = %d overflows window", c.window, got)
		}
	}
}

func TestDialogHeight(t *testing.T) {
	cases := []struct {
		window int
		want   int
	}{
		{0, dialogMinHeight},
		{10, 6},                // tiny: window-margin
		{24, 12},               // half
		{100, dialogMaxHeight}, // capped
	}
	for _, c := range cases {
		if got := DialogHeight(c.window); got != c.want {
			t.Errorf("DialogHeight(%d) = %d, want %d", c.window, got, c.want)
		}
	}
}
