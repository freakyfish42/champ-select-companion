package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LegacyPrefix marked the app's rune page in early test builds; such a page still counts as the app's own.
// The app now knows its page by id (remembered on this PC), so nothing in the client carries the app's name.
const LegacyPrefix = "lømsland"

var roleNames = map[string]string{"top": "Top", "jungle": "Jungle", "mid": "Mid", "adc": "Bot", "support": "Support"}

// PageName is e.g. "Ahri Mid", kept short enough for the client's name field.
func PageName(champ, role string) string {
	n := champ
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
// PageID is the rune page the app wrote, to remember as its own.
type Result struct {
	OK      bool   `json:"ok"`
	Msg     string `json:"msg"`
	Replace []Page `json:"replace,omitempty"`
	PageID  int    `json:"-"`
}

func fail(err error) Result { return Result{Msg: err.Error()} }

type pageBody struct {
	ID              int    `json:"id,omitempty"`
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

// ApplyRunes writes p into the app's own rune page (one of ownIDs, the pages it wrote before) and makes it current.
// With no page of its own and no free slot it changes nothing and returns the player's pages to choose from;
// replaceID is the page the player then chose. dry only describes what would happen.
//
// So you never load in with a blank page:
//   - an existing page is changed in place (one PUT), never deleted and made again, so there is no moment
//     without it; only a free slot gets a new page
//   - every rune is checked against the client's own list first (a patch can make the site and client differ)
//   - afterwards the active page is read back, and Apply only says "set" if the client really has those runes
func (c *Client) ApplyRunes(p RunePage, name string, ownIDs []int, replaceID int, dry bool) Result {
	ids, err := PerkIDs(p)
	if err != nil {
		return fail(err)
	}
	if bad := c.unknownRunes(append([]int{p.Primary.ID, p.Secondary.ID}, ids...)); bad != "" {
		return Result{Msg: "This page has runes your League client doesn't know (" + bad + "), maybe after a new patch. Nothing was changed: set your runes yourself this time."}
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
		if own == nil && (containsInt(ownIDs, pg.ID) || strings.HasPrefix(pg.Name, LegacyPrefix)) {
			own = &pages[i]
		}
	}
	target := 0 // the page to change in place; 0 = make a new one in a free slot
	switch {
	case own != nil:
		target = own.ID
	case replaceID > 0:
		for _, pg := range editable {
			if pg.ID == replaceID {
				target = pg.ID
			}
		}
		if target == 0 {
			return Result{Msg: "That rune page is gone. Pick another one."}
		}
	case len(editable) >= inv.OwnedPageCount:
		return Result{Msg: "All your rune page slots are in use. Pick one to replace:", Replace: editable}
	}
	if dry {
		what := "a new page"
		for _, pg := range pages {
			if target != 0 && pg.ID == target {
				what = "“" + pg.Name + "”"
			}
		}
		return Result{OK: true, Msg: fmt.Sprintf("Test mode: would write %s (%s + %s: %s) over %s.", name, p.Primary.Name, p.Secondary.Name, describe(p), what)}
	}
	body := pageBody{ID: target, Name: name, PrimaryStyleID: p.Primary.ID, SubStyleID: p.Secondary.ID, SelectedPerkIDs: ids, Current: true}
	if target != 0 {
		if err := c.Do("PUT", fmt.Sprintf("/lol-perks/v1/pages/%d", target), body, nil); err != nil {
			return fail(err) // the page is as it was: nothing was deleted
		}
	} else {
		var made Page
		if err := c.Do("POST", "/lol-perks/v1/pages", body, &made); err != nil {
			return fail(err)
		}
		target = made.ID
	}
	if !c.runesActive(target, body) {
		_ = c.Do("PUT", "/lol-perks/v1/currentpage", target, nil)
		if !c.runesActive(target, body) {
			return Result{PageID: target, Msg: "The client didn't take the rune page. Check your runes now, before the game starts!"}
		}
	}
	return Result{OK: true, Msg: "Rune page set: " + p.Keys[0].Name, PageID: target}
}

// unknownRunes names the ids the client doesn't know, or "" when all are known. If the client's list can't be
// read, it doesn't block (the check after writing still catches a page the client didn't take).
func (c *Client) unknownRunes(ids []int) string {
	var perks, styles []struct {
		ID int `json:"id"`
	}
	if c.Do("GET", "/lol-perks/v1/perks", nil, &perks) != nil || c.Do("GET", "/lol-perks/v1/styles", nil, &styles) != nil || len(perks) == 0 || len(styles) == 0 {
		return ""
	}
	known := map[int]bool{}
	for _, x := range append(perks, styles...) {
		known[x.ID] = true
	}
	var bad []string
	for _, id := range ids {
		if !known[id] {
			bad = append(bad, fmt.Sprint(id))
		}
	}
	return strings.Join(bad, ", ")
}

// runesActive reads the active page back: is it page id, with exactly these runes? The client can take a moment.
func (c *Client) runesActive(id int, want pageBody) bool {
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(c.settle)
		}
		var cur struct {
			ID              int   `json:"id"`
			PrimaryStyleID  int   `json:"primaryStyleId"`
			SubStyleID      int   `json:"subStyleId"`
			SelectedPerkIDs []int `json:"selectedPerkIds"`
		}
		if c.Do("GET", "/lol-perks/v1/currentpage", nil, &cur) != nil {
			continue
		}
		if cur.ID == id && cur.PrimaryStyleID == want.PrimaryStyleID && cur.SubStyleID == want.SubStyleID && sameInts(cur.SelectedPerkIDs, want.SelectedPerkIDs) {
			return true
		}
	}
	return false
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
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
