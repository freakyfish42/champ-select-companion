package core

import "strings"

// NextSkill finds the point in the build's skill order (from champ select) you should put in next, given your level
// and the points you have in Q, W, E and R. idx is its place in the order (-1: none left), and spend says you have
// a point to spend right now (otherwise idx is the one for your next level-up).
//
// It follows the plan, not your clicks: if you put a point somewhere else, the plan's first point you haven't
// got yet is next. Points you can't take yet (a basic ability's rank n needs level 2n-1, R needs 6/11/16) are
// skipped. Jayce, Elise, Nidalee and Karma get R for free; their R isn't counted as a spent point.
func NextSkill(order []string, p Player, alias string) (idx int, spend bool) {
	if p.Level < 1 {
		return -1, false
	}
	freeR := FreeR(alias) || (p.Skills[3] > 0 && p.Level < 6) // R at level 1-5 can only have come for free
	spent := p.Skills[0] + p.Skills[1] + p.Skills[2]
	if !freeR {
		spent += p.Skills[3]
	}
	spend = spent < p.Level
	// "next" while you have nothing to spend: what the next level-up should go into
	lv := p.Level
	if !spend {
		lv++
	}
	var planned [4]int
	for i, k := range order {
		s := skillSlot(k)
		if s < 0 || (s == 3 && freeR) {
			continue
		}
		planned[s]++
		if planned[s] <= p.Skills[s] {
			continue // you have this one
		}
		if canLevel(s, p.Skills[s], lv) {
			return i, spend
		}
	}
	return -1, spend
}

// FreeR: champions whose R comes at level 1 and ranks up by itself (no skill points go into it).
func FreeR(alias string) bool {
	switch strings.ToLower(alias) {
	case "jayce", "elise", "nidalee", "karma":
		return true
	}
	return false
}

func skillSlot(k string) int {
	switch strings.ToUpper(strings.TrimSpace(k)) {
	case "Q":
		return 0
	case "W":
		return 1
	case "E":
		return 2
	case "R":
		return 3
	}
	return -1
}

// canLevel: can an ability with `have` points get one more at level lv?
func canLevel(slot, have, lv int) bool {
	if slot == 3 {
		return have < 3 && lv >= 6+5*have
	}
	return have < 5 && lv >= 2*have+1
}
