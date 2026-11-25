package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Pref is the player's last choice for one champion and role.
type Pref struct {
	Keystone int   `json:"keystone,omitempty"`
	Sub      int   `json:"sub,omitempty"` // secondary tree, to tell apart two pages with the same keystone
	Spells   []int `json:"spells,omitempty"`
}

// Prefs remembers picks per "Champion:role" in a small JSON file.
type Prefs struct {
	mu   sync.Mutex
	path string
	M    map[string]Pref `json:"picks"`
}

func PrefKey(champ, role string) string { return champ + ":" + role }

func LoadPrefs(path string) *Prefs {
	p := &Prefs{path: path, M: map[string]Pref{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, p)
		if p.M == nil {
			p.M = map[string]Pref{}
		}
	}
	return p
}

func (p *Prefs) Get(key string) Pref {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.M[key]
}

func (p *Prefs) Set(key string, f func(*Pref)) {
	p.mu.Lock()
	cur := p.M[key]
	f(&cur)
	p.M[key] = cur
	b, _ := json.MarshalIndent(p, "", " ")
	path := p.path
	p.mu.Unlock()
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}

// Order puts the remembered rune page and spell pair first; the rest keep op.gg's order.
func Order(b *Build, pr Pref) (runes, spells []int) {
	for i := range b.Runes {
		runes = append(runes, i)
	}
	for i := range b.Summs {
		spells = append(spells, i)
	}
	front := func(list []int, match func(int) bool) []int {
		for k, i := range list {
			if match(i) {
				return append([]int{i}, append(append([]int{}, list[:k]...), list[k+1:]...)...)
			}
		}
		return list
	}
	if pr.Keystone != 0 {
		runes = front(runes, func(i int) bool {
			return b.Runes[i].Keystone() == pr.Keystone && (pr.Sub == 0 || b.Runes[i].Secondary.ID == pr.Sub)
		})
	}
	if len(pr.Spells) == 2 {
		spells = front(spells, func(i int) bool { return samePair(b.Summs[i].IDs, pr.Spells) })
	}
	return
}

func samePair(a, b []int) bool {
	return len(a) == 2 && len(b) == 2 && ((a[0] == b[0] && a[1] == b[1]) || (a[0] == b[1] && a[1] == b[0]))
}

// Newer reports whether version latest ("v1.2.3") is newer than cur. A "dev" build never updates.
func Newer(cur, latest string) bool {
	if cur == "dev" || cur == "" {
		return false
	}
	pc, pl := parseVer(cur), parseVer(latest)
	if pc == nil || pl == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if pl[i] != pc[i] {
			return pl[i] > pc[i]
		}
	}
	return false
}

func parseVer(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	p := strings.Split(v, ".")
	if len(p) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, s := range p {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}
