# lømsland companion

A small Windows tray app for League of Legends. In champ select it shows a slim panel beside the League client with
[build.lømsland.no](https://build.lømsland.no)'s two most played rune pages and summoner spell pairs for your champion
and role (EUW, Emerald+). Click **Apply** and it sets them in the client. Nothing changes until you click.

- Starts with Windows, sits in the tray, and does nothing until the League client is open.
- The panel docks to the client's right edge (or left, or over its edge if there's no room), shows only while the
  client is in front, and hides when the game starts. Drag it by the header; × hides it for this champ select.
- Rune pages go into one page of its own, named like `lømsland · Ahri Mid`. Your other pages are never touched. If
  every slot is full, it asks which page to replace.
- Spells keep their keys: if Flash is on F, it stays on F.
- It remembers what you picked per champion and role, and shows that first next time.
- No panel in ARAM, Arena and other modes (the stats are ranked Summoner's Rift).
- Updates itself from this repo's releases, never during champ select or a game.

## Install

Download `lomsland-companion.exe` from the latest release and run it. Windows SmartScreen will warn because the app
isn't code-signed: click **More info → Run anyway**. It copies itself to `%LOCALAPPDATA%\lomsland-companion` and
starts from there; you can delete the download.

Tray menu: show the panel again, turn the panel off, **test mode** (Apply only says what it would change), start with
Windows, open the site, open the log folder, check for updates, quit.

To remove it: untick *Start with Windows*, quit, and delete `%LOCALAPPDATA%\lomsland-companion`.

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

```sh
go test ./core        # logic, against a fake League client
./build.sh v1.0.1     # dist/lomsland-companion.exe + .sha256 (needs Go 1.22+ and llvm-windres)
```

Dependencies are vendored as source in `third_party/` (see `go.mod`'s replace lines).

## Release

Create a GitHub release with tag `vX.Y.Z` and attach both `dist/lomsland-companion.exe` and
`dist/lomsland-companion.exe.sha256`. Running copies pick it up within 6 hours (or via *Check for updates*), check the
SHA-256, swap the exe and restart.
