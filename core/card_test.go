package core

import "testing"

func TestCardGate(t *testing.T) {
	run := func(phases ...string) []bool {
		var g CardGate
		var out []bool
		for _, p := range phases {
			out = append(out, g.Update(p))
		}
		return out
	}
	last := func(b []bool) bool { return b[len(b)-1] }
	// your own game, polled with gaps (GameStart can be missed)
	if !last(run("Lobby", "Matchmaking", "ReadyCheck", "ChampSelect", "ChampSelect", "GameStart", "InProgress")) {
		t.Fatal("a game straight from champ select gets the card")
	}
	if !last(run("ChampSelect", "InProgress")) || !last(run("ChampSelect", "InProgress", "Reconnect", "InProgress")) {
		t.Fatal("missed GameStart, or a reconnect, keeps the card")
	}
	// the bug: TFT (no champ select) after a Rift game showed the old build
	if last(run("ChampSelect", "InProgress", "WaitingForStats", "EndOfGame", "Lobby", "Matchmaking", "InProgress")) {
		t.Fatal("TFT after a Rift game must not get the old card")
	}
	if last(run("ChampSelect", "InProgress", "EndOfGame", "None", "InProgress")) {
		t.Fatal("spectating after a game must not get a card")
	}
	if last(run("ChampSelect", "Lobby", "InProgress")) {
		t.Fatal("a game after a dodge must not get the dodged champ select's card")
	}
	if last(run("InProgress")) {
		t.Fatal("app started mid-game: no plan, no card")
	}
}

func TestCardRect(t *testing.T) {
	game := Rect{0, 0, 1920, 1080}
	r := CardRect(game, 1, 0, 0, 88, 100)
	if r != (Rect{16, 118, 316, 206}) {
		t.Fatal("default spot", r)
	}
	if r := CardRect(game, 1.5, 0, 0, 110, 125); r.Right-r.Left != 562 || r.Bottom-r.Top != 165 {
		t.Fatal("size and DPI", r)
	}
	// dragged far off the edge, or the game got smaller: pulled back inside
	if r := CardRect(game, 1, 5000, 5000, 88, 150); r.Right != 1920 || r.Bottom != 1080 {
		t.Fatal("off the bottom right", r)
	}
	if r := CardRect(Rect{100, 100, 1380, 820}, 1, -900, -50, 88, 100); r.Left != 100 || r.Top != 100 {
		t.Fatal("off the top left", r)
	}
	// a saved spot inside is kept exactly
	if r := CardRect(game, 1, 1200, 40, 88, 100); r.Left != 1200 || r.Top != 40 {
		t.Fatal("saved spot", r)
	}
	if r := CardRect(game, 1, 0, 0, 0, 0); r.Bottom-r.Top != 96 {
		t.Fatal("before the page measured itself", r)
	}
}
