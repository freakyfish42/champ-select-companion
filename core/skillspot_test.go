package core

import "testing"

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
