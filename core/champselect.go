package core

import "time"

type Session struct {
	LocalPlayerCellID int        `json:"localPlayerCellId"`
	MyTeam            []Member   `json:"myTeam"`
	TheirTeam         []Member   `json:"theirTeam"`
	Actions           [][]Action `json:"actions"`
	Timer             Timer      `json:"timer"`
}

// Timer is champ select's clock. FINALIZATION is the last part, after everyone has locked in; then the game starts.
type Timer struct {
	Phase    string `json:"phase"`
	LeftInMs int64  `json:"adjustedTimeLeftInPhase"`
}

// SafeMargin: no Apply in the last seconds before the game starts, so a change can't be cut off halfway.
const SafeMargin = 3 * time.Second

// TooLate: is the game about to start? read is when the timer was read, now is the current time.
func (t Timer) TooLate(read, now time.Time) bool {
	switch t.Phase {
	case "GAME_STARTING":
		return true
	case "FINALIZATION":
		return time.Duration(t.LeftInMs)*time.Millisecond-now.Sub(read) < SafeMargin
	}
	return false
}

type Member struct {
	CellID     int    `json:"cellId"`
	ChampionID int    `json:"championId"`
	PickIntent int    `json:"championPickIntent"`
	Position   string `json:"assignedPosition"`
	Spell1     int    `json:"spell1Id"`
	Spell2     int    `json:"spell2Id"`
	Puuid      string `json:"puuid"` // shown for the other team in some queues (Clash), else ""
}

type Action struct {
	ActorCellID  int    `json:"actorCellId"`
	ChampionID   int    `json:"championId"`
	Completed    bool   `json:"completed"`
	IsInProgress bool   `json:"isInProgress"`
	Type         string `json:"type"`
}

// Pick is what the local player has: the champion (locked, hovered or declared), the role and current spells.
type Pick struct {
	ChampionID int
	Locked     bool
	Role       string // top, jungle, mid, adc, support, or "" when the queue has no roles
	Spell1     int
	Spell2     int
}

func (s Session) Mine() Pick {
	var me Member
	for _, m := range s.MyTeam {
		if m.CellID == s.LocalPlayerCellID {
			me = m
			break
		}
	}
	p := Pick{Role: RoleFromPosition(me.Position), Spell1: me.Spell1, Spell2: me.Spell2}
	var hover int
	for _, group := range s.Actions {
		for _, a := range group {
			if a.ActorCellID != s.LocalPlayerCellID || a.Type != "pick" || a.ChampionID <= 0 {
				continue
			}
			if a.Completed {
				p.ChampionID, p.Locked = a.ChampionID, true
			} else if a.IsInProgress {
				hover = a.ChampionID
			}
		}
	}
	switch {
	case p.Locked:
	case hover > 0:
		p.ChampionID = hover
	case me.ChampionID > 0:
		p.ChampionID = me.ChampionID
	default:
		p.ChampionID = me.PickIntent
	}
	return p
}

// EnemyPick is a champion the other team has shown, and who picked it (when the client says).
type EnemyPick struct {
	ChampionID int
	Puuid      string
}

// EnemyPicks are the champions the other team has locked in or shown so far.
func (s Session) EnemyPicks() []EnemyPick {
	var out []EnemyPick
	for _, m := range s.TheirTeam {
		if m.ChampionID > 0 {
			out = append(out, EnemyPick{m.ChampionID, m.Puuid})
		}
	}
	return out
}

// RoleFromPosition maps the client's assigned positions to the site's role names.
func RoleFromPosition(p string) string {
	switch p {
	case "top":
		return "top"
	case "jungle":
		return "jungle"
	case "middle", "mid":
		return "mid"
	case "bottom", "adc":
		return "adc"
	case "utility", "support":
		return "support"
	}
	return ""
}

// SupportedMode: the site's numbers are ranked Summoner's Rift, so only Rift games get suggestions:
// normal Rift games ("CLASSIC") and the Practice Tool ("PRACTICETOOL", also the Rift; owner's request,
// so the app can be tried without queuing).
func SupportedMode(mode string) bool { return mode == "CLASSIC" || mode == "PRACTICETOOL" }
