package core

import (
	"errors"
	"math"
)

// SkillSpot is where the game's Q and R icons are (their centres): x from the game window's horizontal middle,
// y up from its bottom, both as a share of the game window's height. League's HUD is centred and grows with the
// window's height, so one spot fits every resolution with the same aspect and HUD scale. W and E are evenly
// between Q and R.
type SkillSpot struct {
	QX float64 `json:"qx"`
	QY float64 `json:"qy"`
	RX float64 `json:"rx"`
	RY float64 `json:"ry"`
	// the HUD Scale it was lined up at (nil: unknown, from before the app read it)
	HUD *float64 `json:"hud,omitempty"`
}

// HUDSpot is where League puts the ability bar at HUD Scale s (0 to 1). Measured on the owner's PC on 2026-11-26
// at HUD Scale 0, 9 and 100 (4:3, 1280x960) and checked in a 16:9 window: the bar is sized by the game's height,
// grows linearly with the scale, and R's centre stays in the middle of the screen. Within a pixel at all four.
func HUDSpot(s float64) SkillSpot {
	s = math.Max(0, math.Min(1, s))
	p := 0.0405 + 0.0213*s // gap between two icons' centres
	y := 0.0051 + 1.46*p   // the icons' centre, up from the bottom
	const rx = -0.0013     // R's centre, from the middle
	return SkillSpot{QX: rx - 3*p, QY: y, RX: rx, RY: y}
}

// ChooseSpot picks where the marker goes: a spot the player lined up at the current HUD Scale wins (they may
// know better, e.g. after a HUD change in a patch); otherwise the measured layout for the current HUD Scale;
// a lined-up spot of unknown scale only when the scale can't be read. ok false: nowhere known.
func ChooseSpot(manual *SkillSpot, scale float64, haveScale bool) (SkillSpot, bool) {
	if manual != nil && manual.Valid() {
		if manual.HUD != nil && haveScale && math.Abs(*manual.HUD-scale) < 0.005 {
			return *manual, true
		}
		if !haveScale {
			return *manual, true
		}
	}
	if haveScale {
		return HUDSpot(scale), true
	}
	return DefaultSkillSpot(), false
}

// DefaultSkillSpot: where the boxes start when lining up without a known HUD Scale (the middle of the range).
func DefaultSkillSpot() SkillSpot { return HUDSpot(0.5) }

// Valid: R to the right of Q, a sane distance apart, about level.
func (s SkillSpot) Valid() bool {
	d := s.RX - s.QX
	return d >= 0.04 && d <= 0.6 && math.Abs(s.RY-s.QY) <= d/3 &&
		s.QY > 0 && s.QY < 0.5 && s.RY > 0 && s.RY < 0.5 && math.Abs(s.QX) < 1 && math.Abs(s.RX) < 1
}

func centre(r Rect) (float64, float64) {
	return float64(r.Left+r.Right) / 2, float64(r.Top+r.Bottom) / 2
}

// SpotFrom turns the two boxes the player lined up (over Q and over R) into a SkillSpot.
func SpotFrom(game, q, r Rect) (SkillSpot, error) {
	h := float64(game.Bottom - game.Top)
	if h < 100 {
		return SkillSpot{}, errors.New("the game window is too small")
	}
	mid := float64(game.Left+game.Right) / 2
	qx, qy := centre(q)
	rx, ry := centre(r)
	s := SkillSpot{QX: (qx - mid) / h, QY: (float64(game.Bottom) - qy) / h, RX: (rx - mid) / h, RY: (float64(game.Bottom) - ry) / h}
	if !s.Valid() {
		return SkillSpot{}, errors.New("put the blue box on Q and the red box on R (R is to the right of Q, on the same row)")
	}
	return s, nil
}

// SkillBox is the square over ability slot 0..3 (Q W E R) in screen pixels: a little smaller than the gap
// between two icons' centres, so neighbouring boxes don't touch.
func SkillBox(game Rect, s SkillSpot, slot int) Rect {
	h := float64(game.Bottom - game.Top)
	mid := float64(game.Left+game.Right) / 2
	t := float64(slot) / 3
	cx := mid + (s.QX+(s.RX-s.QX)*t)*h
	cy := float64(game.Bottom) - (s.QY+(s.RY-s.QY)*t)*h
	pitch := math.Hypot(s.RX-s.QX, s.RY-s.QY) * h / 3
	side := math.Max(16, math.Round(pitch*0.95))
	l := int32(math.Round(cx - side/2))
	tp := int32(math.Round(cy - side/2))
	return Rect{Left: l, Top: tp, Right: l + int32(side), Bottom: tp + int32(side)}
}
