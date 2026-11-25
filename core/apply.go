package core

import (
	"errors"
	"fmt"
	"strings"
)

// PagePrefix marks the one rune page the app owns. Pages without it are never changed unless the player picks one to replace.
const PagePrefix = "lømsland"

var roleNames = map[string]string{"top": "Top", "jungle": "Jungle", "mid": "Mid", "adc": "Bot", "support": "Support"}

// PageName is e.g. "lømsland · Ahri Mid", kept short enough for the client's name field.
func PageName(champ, role string) string {
	n := PagePrefix + " · " + champ
	if r := roleNames[role]; r != "" {
		n += " " + r
	}
	if rs := []rune(n); len(rs) > 25 {
		n = string(rs[:25])
	}
	return n
}

type Page struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	IsDeletable bool   `json:"isDeletable"`
	IsEditable  bool   `json:"isEditable"`
	Current     bool   `json:"current"`
}

// Result is what the panel shows after a click. Replace lists the player's pages when every slot is in use.
type Result struct {
	OK      bool   `json:"ok"`
	Msg     string `json:"msg"`
	Replace []Page `json:"replace,omitempty"`
}

func fail(err error) Result { return Result{Msg: err.Error()} }

type pageBody struct {
	Name            string `json:"name"`
	PrimaryStyleID  int    `json:"primaryStyleId"`
	SubStyleID      int    `json:"subStyleId"`
	SelectedPerkIDs []int  `json:"selectedPerkIds"`
	Current         bool   `json:"current"`
}

// PerkIDs is the order the client wants: 4 primary runes, 2 secondary runes, 3 shards.
func PerkIDs(p RunePage) ([]int, error) {
	if len(p.Keys) != 4 || len(p.Subs) != 2 || len(p.Shards) != 3 || p.Primary.ID == 0 || p.Secondary.ID == 0 {
		return nil, errors.New("this rune page is incomplete in op.gg's data")
	}
	ids := []int{}
	for _, g := range [][]RuneRef{p.Keys, p.Subs, p.Shards} {
		for _, r := range g {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

func describe(p RunePage) string {
	names := []string{}
	for _, r := range append(append([]RuneRef{}, p.Keys...), p.Subs...) {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

// ApplyRunes writes p into the app's own rune page and makes it the current page.
// With no page of its own and no free slot it changes nothing and returns the player's pages to choose from;
// replaceID is the page the player then chose. dry only describes what would happen.
func (c *Client) ApplyRunes(p RunePage, name string, replaceID int, dry bool) Result {
	ids, err := PerkIDs(p)
	if err != nil {
		return fail(err)
	}
	var pages []Page
	if err := c.Do("GET", "/lol-perks/v1/pages", nil, &pages); err != nil {
		return fail(err)
	}
	var inv struct {
		OwnedPageCount int `json:"ownedPageCount"`
	}
	if err := c.Do("GET", "/lol-perks/v1/inventory", nil, &inv); err != nil {
		return fail(err)
	}
	var own *Page
	var editable []Page
	for i := range pages {
		pg := pages[i]
		if !pg.IsDeletable {
			continue // Riot's preset pages
		}
		editable = append(editable, pg)
		if own == nil && strings.HasPrefix(pg.Name, PagePrefix) {
			own = &pages[i]
		}
	}
	remove := 0
	switch {
	case own != nil:
		remove = own.ID
	case replaceID > 0:
		for _, pg := range editable {
			if pg.ID == replaceID {
				remove = pg.ID
			}
		}
		if remove == 0 {
			return Result{Msg: "That rune page is gone. Pick another one."}
		}
	case len(editable) >= inv.OwnedPageCount:
		return Result{Msg: "All your rune page slots are in use. Pick one to replace:", Replace: editable}
	}
	if dry {
		what := "a new page"
		if remove != 0 {
			for _, pg := range pages {
				if pg.ID == remove {
					what = "“" + pg.Name + "”"
				}
			}
		}
		return Result{OK: true, Msg: fmt.Sprintf("Test mode: would write %s (%s + %s: %s) over %s.", name, p.Primary.Name, p.Secondary.Name, describe(p), what)}
	}
	if remove != 0 {
		if err := c.Do("DELETE", fmt.Sprintf("/lol-perks/v1/pages/%d", remove), nil, nil); err != nil {
			return fail(err)
		}
	}
	var made Page
	if err := c.Do("POST", "/lol-perks/v1/pages", pageBody{Name: name, PrimaryStyleID: p.Primary.ID, SubStyleID: p.Secondary.ID, SelectedPerkIDs: ids, Current: true}, &made); err != nil {
		return fail(err)
	}
	if made.ID != 0 && !made.Current {
		_ = c.Do("PUT", "/lol-perks/v1/currentpage", made.ID, nil)
	}
	return Result{OK: true, Msg: "Rune page set: " + p.Keys[0].Name}
}

// SpellSlots keeps each spell on the key it's already on, so Flash stays on D or F as the player likes it.
func SpellSlots(cur1, cur2, a, b int) (int, int) {
	if a == cur2 || b == cur1 {
		return b, a
	}
	return a, b
}

func (c *Client) ApplySpells(pair []int, names map[int]string, cur1, cur2 int, dry bool) Result {
	if len(pair) != 2 {
		return Result{Msg: "this spell pair is incomplete"}
	}
	s1, s2 := SpellSlots(cur1, cur2, pair[0], pair[1])
	if dry {
		n := func(id int) string {
			if names[id] != "" {
				return names[id]
			}
			return fmt.Sprintf("spell %d", id)
		}
		return Result{OK: true, Msg: fmt.Sprintf("Test mode: would set %s on D and %s on F.", n(s1), n(s2))}
	}
	if err := c.Do("PATCH", "/lol-champ-select/v1/session/my-selection", map[string]int{"spell1Id": s1, "spell2Id": s2}, nil); err != nil {
		return fail(err)
	}
	return Result{OK: true, Msg: "Summoner spells set"}
}
