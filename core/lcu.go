// Package core holds everything that doesn't need Windows: talking to the League client,
// reading champ select, building rune pages and spell picks, the site's build data,
// remembered picks and version checks. It is tested on any OS with a fake client.
package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Creds come from the League client's lockfile: "LeagueClient:<pid>:<port>:<password>:https".
type Creds struct {
	PID      int
	Port     int
	Password string
}

func ParseLockfile(s string) (Creds, error) {
	p := strings.Split(strings.TrimSpace(s), ":")
	if len(p) < 5 {
		return Creds{}, fmt.Errorf("lockfile has %d parts, want 5", len(p))
	}
	pid, err1 := strconv.Atoi(p[1])
	port, err2 := strconv.Atoi(p[2])
	if err1 != nil || err2 != nil || port <= 0 || p[3] == "" {
		return Creds{}, fmt.Errorf("lockfile is not in the expected format")
	}
	return Creds{PID: pid, Port: port, Password: p[3]}, nil
}

// Client talks to the League client's local API (LCU) on 127.0.0.1.
type Client struct {
	base string
	auth string
	http *http.Client
}

// NewClient connects to the client on this PC (127.0.0.1 only), accepting only a certificate signed by Riot's local root.
func NewClient(c Creds) *Client {
	tr := &http.Transport{
		Proxy:           nil, // never send local traffic through a proxy
		TLSClientConfig: RiotTLS(),
		MaxIdleConns:    4,
	}
	return NewClientURL(fmt.Sprintf("https://127.0.0.1:%d", c.Port), c.Password, &http.Client{Transport: tr, Timeout: 5 * time.Second})
}

func NewClientURL(base, password string, hc *http.Client) *Client {
	return &Client{base: strings.TrimRight(base, "/"), auth: "Basic " + base64.StdEncoding.EncodeToString([]byte("riot:"+password)), http: hc}
}

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	msg := e.Body
	var m struct{ Message string }
	if json.Unmarshal([]byte(e.Body), &m) == nil && m.Message != "" {
		msg = m.Message
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return fmt.Sprintf("League client answered %d: %s", e.Status, msg)
}

// Do sends a request; body is sent as JSON when not nil, and the answer is decoded into out when not nil.
func (c *Client) Do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return &HTTPError{Status: res.StatusCode, Body: string(b)}
	}
	if out != nil && len(bytes.TrimSpace(b)) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Phase is the client's game flow phase: "None", "Lobby", "Matchmaking", "ReadyCheck", "ChampSelect", "InProgress", ...
func (c *Client) Phase() (string, error) {
	var p string
	err := c.Do("GET", "/lol-gameflow/v1/gameflow-phase", nil, &p)
	return p, err
}

// GameMode of the game being set up: "CLASSIC" (Summoner's Rift), "ARAM", "CHERRY" (Arena), ...
func (c *Client) GameMode() (string, error) {
	var s struct {
		Map      struct{ GameMode string } `json:"map"`
		GameData struct {
			Queue struct{ GameMode string } `json:"queue"`
		} `json:"gameData"`
	}
	if err := c.Do("GET", "/lol-gameflow/v1/session", nil, &s); err != nil {
		return "", err
	}
	if s.GameData.Queue.GameMode != "" {
		return s.GameData.Queue.GameMode, nil
	}
	return s.Map.GameMode, nil
}

// Champion as the client knows it. Alias is the Data Dragon id (e.g. "MonkeyKing" for Wukong).
type Champion struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

func (c *Client) Champions() (map[int]Champion, error) {
	var list []Champion
	if err := c.Do("GET", "/lol-game-data/assets/v1/champion-summary.json", nil, &list); err != nil {
		return nil, err
	}
	m := map[int]Champion{}
	for _, ch := range list {
		if ch.ID > 0 {
			m[ch.ID] = ch
		}
	}
	return m, nil
}

func (c *Client) Session() (Session, error) {
	var s Session
	err := c.Do("GET", "/lol-champ-select/v1/session", nil, &s)
	return s, err
}
