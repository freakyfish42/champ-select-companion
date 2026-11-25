package core

// InGamePhase: the loading screen and the game itself.
func InGamePhase(p string) bool { return p == "GameStart" || p == "InProgress" || p == "Reconnect" }

// CardGate decides whether a game gets the in-game build card: only a game you entered straight from your own
// champ select. Not TFT (no champ select), not spectating, not a game after a dodge: those would otherwise show
// the build left over from an earlier champ select.
type CardGate struct{ inSelect, armed bool }

// Update takes every gameflow phase the app sees, in order, and says whether this game gets the card.
func (g *CardGate) Update(phase string) bool {
	switch {
	case phase == "ChampSelect":
		g.inSelect, g.armed = true, false
	case InGamePhase(phase):
		if g.inSelect {
			g.armed = true
		}
		g.inSelect = false
	default: // lobby, matchmaking, end of game, …: the plan is used up
		g.inSelect, g.armed = false, false
	}
	return g.armed
}

// Rect is a window rectangle in screen pixels.
type Rect struct{ Left, Top, Right, Bottom int32 }

// CardRect places the card over the game: the saved offset from the game's top-left corner (dx, dy; 0,0 = the
// default spot, top left a little down), sized by the tray's size setting (percent) and the card's measured height
// (CSS px, the page measures itself), and always kept inside the game window so it can never get lost.
func CardRect(game Rect, scale float64, dx, dy, cssH int32, size int) Rect {
	if size <= 0 {
		size = 100
	}
	if cssH < 40 {
		cssH = int32(96 * size / 100)
	}
	w, h := int32(300*float64(size)/100*scale), int32(float64(cssH)*scale)
	if dx == 0 && dy == 0 {
		dx, dy = int32(16*scale), int32(float64(game.Bottom-game.Top)*0.11)
	}
	l, t := game.Left+dx, game.Top+dy
	if l+w > game.Right {
		l = game.Right - w
	}
	if t+h > game.Bottom {
		t = game.Bottom - h
	}
	if l < game.Left {
		l = game.Left
	}
	if t < game.Top {
		t = game.Top
	}
	return Rect{Left: l, Top: t, Right: l + w, Bottom: t + h}
}
