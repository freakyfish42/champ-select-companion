package core

import "fmt"

// Objective is a neutral objective's first spawn on Summoner's Rift: a fixed rule of the game, the same in
// every match (Season 2026, patch 26.x: Atakhan removed, Baron back at 20:00).
type Objective struct {
	Name string
	At   int // game time in seconds
}

// Objectives: only the fixed first spawns. Later spawns after a kill are left out on purpose: they'd come from what
// happened in this match, which Riot's third-party rules don't allow an app to track for you.
var Objectives = []Objective{
	{"Dragon", 5 * 60},
	{"Void Grubs", 8 * 60},
	{"Rift Herald", 15 * 60},
	{"Baron", 20 * 60},
}

// CountdownLead: how long before a spawn the countdown shows; CountdownUp: how long "is up" stays after.
const (
	CountdownLead = 60
	CountdownUp   = 5
)

// Countdown says what the objective timer shows at game time t (seconds): an objective spawning within the next
// minute (left > 0), or one that spawned in the last few seconds (left <= 0). ok false: nothing to show.
func Countdown(t float64) (o Objective, left float64, ok bool) {
	for _, o := range Objectives {
		left := float64(o.At) - t
		if left <= CountdownLead && left > -CountdownUp {
			return o, left, true
		}
	}
	return Objective{}, 0, false
}

// CountdownText: "Dragon 0:42", or "Dragon is up".
func CountdownText(o Objective, left float64) string {
	if left <= 0 {
		return o.Name + " is up"
	}
	s := int(left + 0.999) // 0.2 s left still shows 0:01
	return fmt.Sprintf("%s %d:%02d", o.Name, s/60, s%60)
}
