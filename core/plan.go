package core

// SelectPlan is what the app does when champ select starts. It is the one place that decides
// whether the panel shows and whether Clash mode is on, so the rules live (and are tested) here.
type SelectPlan struct {
	Panel       bool // show the panel with rune pages, spells and the lane matchup
	ClashClient bool // read this round's opponent from the League client's Clash data
	ClashHelper bool // read the team's saved matches from the Clash helper
}

// PlanFor: suggestions only on Summoner's Rift; Clash mode only when the client says the game is Clash
// (and it's on the Rift: ARAM Clash gets nothing).
func PlanFor(g Game) SelectPlan {
	rift := SupportedMode(g.Mode)
	clash := g.Mode == "CLASSIC" && g.Clash // Summoner's Rift 5v5 Clash only (not the Practice Tool)
	return SelectPlan{Panel: rift, ClashClient: clash, ClashHelper: clash}
}
