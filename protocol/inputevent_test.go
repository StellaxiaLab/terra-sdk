package protocol

import "testing"

// The mapping has to match what Windows itself does to absolute injection,
// because the commonest sink hands these numbers straight to SendInput. These
// three points were measured on a 2560x1440 display: 32768 landed on 1280,
// 16384 on 640, and 0 on 0.
func TestToPixelsMatchesMeasuredAbsolutePlacement(t *testing.T) {
	const width, height = 2560, 1440
	for _, testCase := range []struct {
		position PointerPosition
		wantX    int
		wantY    int
	}{
		{PointerPosition{X: 32768, Y: 32768}, 1280, 720},
		{PointerPosition{X: 16384, Y: 16384}, 640, 360},
		{PointerPosition{X: 0, Y: 0}, 0, 0},
		{PointerPosition{X: PointerPositionMax, Y: PointerPositionMax}, 2559, 1439},
	} {
		x, y := testCase.position.ToPixels(width, height)
		if x != testCase.wantX || y != testCase.wantY {
			t.Errorf("%+v -> %d,%d, want %d,%d", testCase.position, x, y, testCase.wantX, testCase.wantY)
		}
	}
}

// A position must survive the trip to a display of a different size and back
// to roughly where it started. Exactness is impossible — normalized space is
// coarser than a large display in the middle of its range — but the drift has
// to stay under a pixel or two, because a cursor that lands a little off on
// every hop walks away from where the person put it.
func TestNormalizeRoundTripStaysPutAcrossDisplaySizes(t *testing.T) {
	sizes := [][2]int{{2560, 1440}, {1920, 1080}, {3840, 2160}, {1366, 768}}
	for _, size := range sizes {
		width, height := size[0], size[1]
		for _, pixel := range [][2]int{{0, 0}, {1, 1}, {width / 2, height / 2}, {width - 1, height - 1}} {
			position := NormalizePixels(pixel[0], pixel[1], width, height)
			x, y := position.ToPixels(width, height)
			if difference(x, pixel[0]) > 1 || difference(y, pixel[1]) > 1 {
				t.Errorf("%dx%d: %d,%d -> %+v -> %d,%d (drifted)", width, height, pixel[0], pixel[1], position, x, y)
			}
		}
	}
}

// The edges are the whole point of an edge hop, so they get their own check:
// the far edge must not wrap to the near one, and the near one must not go
// negative.
func TestNormalizeClampsToTheDisplay(t *testing.T) {
	position := NormalizePixels(-5, -5, 1920, 1080)
	if position.X != 0 || position.Y != 0 {
		t.Errorf("off-screen negative became %+v, want 0,0", position)
	}
	position = NormalizePixels(9999, 9999, 1920, 1080)
	if position.X != PointerPositionMax || position.Y != PointerPositionMax {
		t.Errorf("off-screen positive became %+v, want the maximum", position)
	}
}

func TestToPixelsRefusesAnEmptyDisplay(t *testing.T) {
	if x, y := (PointerPosition{X: 32768, Y: 32768}).ToPixels(0, 0); x != 0 || y != 0 {
		t.Errorf("got %d,%d for a zero-sized display, want 0,0", x, y)
	}
}

func difference(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
