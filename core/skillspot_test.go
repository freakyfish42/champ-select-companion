package core

import (
	"math"
	"testing"
)

func TestSkillSpot(t *testing.T) {
	game := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	// Q centred at (870,1000), R at (1050,1000): 60 px apart per slot
	q := Rect{Left: 845, Top: 975, Right: 895, Bottom: 1025}
	r := Rect{Left: 1025, Top: 975, Right: 1075, Bottom: 1025}
	s, err := SpotFrom(game, q, r)
	if err != nil {
		t.Fatal(err)
	}
	for slot, cx := range []int32{870, 930, 990, 1050} {
		b := SkillBox(game, s, slot)
		if (b.Left+b.Right)/2 != cx || (b.Top+b.Bottom)/2 != 1000 || b.Right-b.Left != 57 {
			t.Errorf("slot %d: %+v", slot, b)
		}
	}
	// same HUD at 1440p on a second monitor: everything x 4/3, moved
	g2 := Rect{Left: 1920, Top: 0, Right: 1920 + 2560, Bottom: 1440}
	if b := SkillBox(g2, s, 0); (b.Left+b.Right)/2 != 1920+1280-120 || (b.Top+b.Bottom)/2 != 1440-107 {
		t.Errorf("1440p: %+v", b)
	}
	// swapped, far apart, or on different rows: refused
	for _, bad := range [][2]Rect{{r, q}, {q, Rect{Left: 1800, Top: 975, Right: 1850, Bottom: 1025}}, {q, Rect{Left: 1025, Top: 500, Right: 1075, Bottom: 550}}} {
		if _, err := SpotFrom(game, bad[0], bad[1]); err == nil {
			t.Errorf("should refuse %+v", bad)
		}
	}
	if !DefaultSkillSpot().Valid() {
		t.Error("the default must be valid")
	}
}

// The owner's screenshots (2026-11-26), in screen pixels: icon centres Q..R and their row.
func TestHUDSpotMatchesScreenshots(t *testing.T) {
	for _, c := range []struct {
		name       string
		scale      float64
		game       Rect
		qx, rx, cy float64
	}{
		{"4:3 HUD 0", 0, Rect{0, 0, 1269, 951}, 517.5, 633.1, 889.9},
		{"4:3 HUD 9", 0.09, Rect{0, 0, 1269, 951}, 512, 633, 888},
		{"4:3 HUD 100", 1, Rect{0, 0, 1269, 951}, 457.5, 633.75, 860.4},
		{"16:9 window HUD 0", 0, Rect{7, 106, 1261, 820}, 546.25, 634.4, 773.4},
	} {
		s := HUDSpot(c.scale)
		q, r := SkillBox(c.game, s, 0), SkillBox(c.game, s, 3)
		qx, qy := centre(q)
		rx, _ := centre(r)
		if math.Abs(qx-c.qx) > 2 || math.Abs(rx-c.rx) > 2 || math.Abs(qy-c.cy) > 2 {
			t.Errorf("%s: Q %.1f R %.1f y %.1f, screenshot Q %.1f R %.1f y %.1f", c.name, qx, rx, qy, c.qx, c.rx, c.cy)
		}
	}
}

func TestChooseSpot(t *testing.T) {
	nine, half := 0.09, 0.5
	lined := SkillSpot{QX: -0.2, QY: 0.07, RX: -0.05, RY: 0.07, HUD: &nine}
	old := SkillSpot{QX: -0.2, QY: 0.07, RX: -0.05, RY: 0.07}
	if s, ok := ChooseSpot(&lined, 0.09, true); !ok || s.QX != -0.2 {
		t.Error("lined up at this HUD scale: use it")
	}
	if s, ok := ChooseSpot(&lined, half, true); !ok || s != HUDSpot(half) {
		t.Error("HUD scale changed since lining up: use the measured layout")
	}
	if s, ok := ChooseSpot(&old, half, true); !ok || s != HUDSpot(half) {
		t.Error("lined up before scales were known: the measured layout wins")
	}
	if s, ok := ChooseSpot(&old, 0, false); !ok || s.QX != -0.2 {
		t.Error("no scale readable: the lined-up spot")
	}
	if _, ok := ChooseSpot(nil, 0, false); ok {
		t.Error("nothing known: no marker")
	}
	if s, ok := ChooseSpot(nil, 0.3, true); !ok || s != HUDSpot(0.3) {
		t.Error("nothing lined up: the measured layout")
	}
	for _, f := range []float64{0, 0.5, 1} {
		if !HUDSpot(f).Valid() {
			t.Error("measured layout must be valid", f)
		}
	}
}

func TestGameHUDScale(t *testing.T) {
	if s, ok := GameHUDScale("[General]\nWindowMode=2\n[HUD]\nMinimapScale=0.3300\nGlobalScale=0.0900\n"); !ok || s != 0.09 {
		t.Fatal(s, ok)
	}
	if _, ok := GameHUDScale("[General]\nGlobalScale=0.5\n"); ok {
		t.Error("only [HUD] counts")
	}
	if _, ok := GameHUDScale("[HUD]\nGlobalScale=7\n"); ok {
		t.Error("out of range")
	}
}
