package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNextSkill(t *testing.T) {
	// a common order: Q W E Q Q R Q E Q E R E E W W R W W
	ord := strings.Fields("Q W E Q Q R Q E Q E R E E W W R W W")
	pl := func(lv, q, w, e, r int) Player { return Player{Level: lv, Skills: [4]int{q, w, e, r}} }
	for _, c := range []struct {
		name  string
		p     Player
		alias string
		idx   int
		spend bool
	}{
		{"level 1, nothing yet", pl(1, 0, 0, 0, 0), "", 0, true},
		{"level 1 spent: W is next at 2", pl(1, 1, 0, 0, 0), "", 1, false},
		{"level 2 unspent", pl(2, 1, 0, 0, 0), "", 1, true},
		{"level 6: R", pl(6, 3, 1, 1, 0), "", 5, true},
		{"level 6 done: Q at 7", pl(6, 3, 1, 1, 1), "", 6, false},
		{"put W instead of E at 3: E still next", pl(3, 1, 2, 0, 0), "", 2, false},
		{"went off plan, a point left: plan's first missing", pl(4, 1, 2, 0, 0), "", 2, true},
		{"R planned at 6 but you're 5: skip to Q", pl(5, 2, 1, 1, 0), "", 4, true},
		{"level 18 all spent", pl(18, 5, 5, 5, 3), "", -1, false},
		{"Q maxed early off plan: next Q in plan skipped", pl(9, 5, 1, 2, 0), "", 5, true},
		{"Jayce at 6: Q can't go to 4 yet, so E", pl(6, 3, 1, 1, 2), "Jayce", 7, true},
		{"Jayce at 1, R=1 free", pl(1, 0, 0, 0, 1), "Jayce", 0, true},
		{"unknown free-R champ at level 2", pl(2, 1, 0, 0, 1), "", 1, true},
		{"Udyr: R at level 1 is a normal point", pl(1, 0, 0, 0, 1), "Udyr", 0, false},
		{"Udyr: R before 6 allowed", Player{Level: 3, Skills: [4]int{1, 1, 0, 0}}, "Udyr", 2, true},
		{"not loaded", pl(0, 0, 0, 0, 0), "", -1, false},
	} {
		idx, spend := NextSkill(ord, c.p, c.alias)
		if idx != c.idx || spend != c.spend {
			t.Errorf("%s: got %d %v, want %d %v", c.name, idx, spend, c.idx, c.spend)
		}
	}
	u := strings.Fields("R Q R W R E R Q R Q R Q W W W W E E")
	if i, s := NextSkill(u, pl(3, 1, 0, 0, 1), "Udyr"); i != 2 || !s {
		t.Error("Udyr: third point is R (rank 2 at level 3)", i, s)
	}
	if i, s := NextSkill(nil, pl(3, 0, 0, 0, 0), ""); i != -1 || !s {
		t.Error("no order: nothing to point at, but a point to spend", i, s)
	}
}

func TestLivePlayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"abilities":{"Passive":{"displayName":"x"},"Q":{"abilityLevel":3,"displayName":"a"},"W":{"abilityLevel":1},"E":{"abilityLevel":1},"R":{"abilityLevel":1}},"level":6,"currentGold":1}`))
	}))
	defer srv.Close()
	p, err := NewLiveURL(srv.URL, srv.Client()).Player()
	if err != nil || p != (Player{Level: 6, Skills: [4]int{3, 1, 1, 1}}) {
		t.Fatal(p, err)
	}
}
