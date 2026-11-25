# Champ Select Companion

A small Windows tray app for League of Legends. In champ select it shows a slim panel beside the League client with
[build.lømsland.no](https://build.lømsland.no)'s two most played rune pages and summoner spell pairs for your champion
and role (EUW, Emerald+). Click **Apply** and it sets them in the client. Nothing changes until you click.

- Starts with Windows, sits in the tray, and does nothing until the League client is open.
- The panel docks to the client's right edge (or left, or over its edge if there's no room), shows only while the
  client is in front, and hides when the game starts. Drag it by the header; × hides it for this champ select.
- Rune pages go into one page of its own, named just like `Ahri Mid` (the app knows it by id, so its name never shows in
  the client). Your other pages are never touched. If
  every slot is full, it asks which page to replace.
- Spells keep their keys: if Flash is on F, it stays on F.
- It remembers what you picked per champion and role, and shows that first next time.
- **Lane matchup**: once the enemy laner is picked, the panel guesses who's in your lane (from the site's role stats)
  and shows your win rate against them, with how many games that's based on.
- **Clash scouting** (only when the client says the game is Clash): reads this round's opponent from the League client's
  own Clash data (your team → bracket → the other team's roster with their registered positions; their champions from the
  client's scouting data, or else their last 20 games), and shows each enemy's role, three most played champions and
  three ban ideas. The team's saved matches from the Clash helper (clash.lømsland.no) are in the same list, and are
  used when the client has nothing yet. In Clash the lane matchup uses the registered positions instead of guessing.
  The client's Clash endpoints are undocumented, so during a Clash every raw answer is written to `log.txt`.
- No panel in ARAM, Arena and other modes (the stats are ranked Summoner's Rift).
- Updates itself from this repo's releases, never during champ select or a game.

## Install

Download `champ-select-companion.exe` from the latest release and run it. Windows SmartScreen will warn because the app
isn't code-signed: click **More info → Run anyway**. It copies itself to `%LOCALAPPDATA%\champ-select-companion` and
starts from there; you can delete the download.

Tray menu: show the panel again, turn the panel off, **test mode** (Apply only says what it would change), start with
Windows, open the site, open the log folder, check for updates, quit.

To remove it: untick *Start with Windows*, quit, and delete `%LOCALAPPDATA%\champ-select-companion`.

## How it works

The League client runs a local API on 127.0.0.1 (the "LCU"). The app finds `LeagueClientUx.exe`, reads the
`lockfile` next to it for the port and password, and polls the game flow phase (every 2 s) and, in champ select, the
session (every 0.6 s). It reads your champion (locked, hovered, or declared) and assigned position, fetches
`/api/build` from build.lømsland.no, and on Apply calls:

- `DELETE /lol-perks/v1/pages/{own page}` + `POST /lol-perks/v1/pages` (`current: true`)
- `PATCH /lol-champ-select/v1/session/my-selection` with `spell1Id` / `spell2Id`

This is the same rune and spell import that Blitz, op.gg and Porofessor use. It only reads champ select and
writes your own rune page and spells; it never touches the game.

The panel is a WebView2 window (part of Windows 10/11) showing `panel.html`; the tray uses `fyne.io/systray`.

## Build

`VISION.md` lists what the app must always be (nothing written without Apply, only runes and spells written, no app
name in the client, Clash mode only in Clash, …). Each point has a test in `core/vision_test.go`, and `build.sh`
won't build if any test fails.

```sh
go test ./core        # logic, against a fake League client
./build.sh v1.0.1     # dist/champ-select-companion.exe + .sha256 (needs Go 1.22+ and llvm-windres)
```

Dependencies are vendored as source in `third_party/` (see `go.mod`'s replace lines).

## Release (signed)

Updates are signed with an Ed25519 key that only exists on the owner's PC. The app has the public key built in
(`updatekey.go`) and installs an update only when `champ-select-companion.exe.sig` verifies for exactly that version tag,
so neither a taken-over GitHub account nor an old release passed off as new can push code to the team.

1. `./build.sh vX.Y.Z`
2. Drag `dist/champ-select-companion.exe` onto `companion-sign.exe` on the owner's PC. It writes `champ-select-companion.exe.sig`.
3. Create a GitHub release with tag `vX.Y.Z` and attach both files.

The first time, double-click `companion-sign.exe` to create the key (`%APPDATA%\companion-sign\signing.key`; a key made under the old name `lomsland-sign` is still used) and see its
public part. Back the key file up: without it no further updates can be signed.

## Security notes

- The League client's local API is only trusted when its certificate is signed by Riot's local root
  (`riotgames.pem` from RiotGames/leaguedirector, built in). Riot's certificates may use SHA-1, so that one signature
  check is done by hand.
- Everything from build.lømsland.no is escaped before it's shown. The panel can only call: apply rune page, apply
  spells, open the site, move and hide the panel.
- The panel's fonts are built in (SIL Open Font License, see `fonts/`). The app contacts only build.lømsland.no, Riot's
  Data Dragon (icons) and GitHub (updates).
- The exe isn't code-signed (that needs a paid certificate), so SmartScreen warns on the first run. Only download it
  from this repo's releases.
