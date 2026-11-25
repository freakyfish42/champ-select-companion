package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SendReport posts a setup report to the build site, which forwards it to the team's private Discord
// channel. The app calls it only when the player clicks Yes on "Send it to the team's Discord?".
func SendReport(hc *http.Client, site, from, report string) error {
	body, _ := json.Marshal(map[string]string{"from": from, "report": report})
	req, err := http.NewRequest("POST", strings.TrimRight(site, "/")+"/api/report", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the build site: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&r)
	if resp.StatusCode == 200 && r.OK {
		return nil
	}
	if r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	return fmt.Errorf("the build site answered %s", resp.Status)
}

// RiotID is the logged-in player's "Name#TAG", or "" when it isn't known. It names the sender of a report.
func (c *Client) RiotID() string {
	var s struct {
		GameName string `json:"gameName"`
		TagLine  string `json:"tagLine"`
	}
	if c.Do("GET", "/lol-summoner/v1/current-summoner", nil, &s) != nil || s.GameName == "" {
		return ""
	}
	if s.TagLine == "" {
		return s.GameName
	}
	return s.GameName + "#" + s.TagLine
}
