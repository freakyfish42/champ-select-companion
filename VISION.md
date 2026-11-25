# What Champ Select Companion must always be

These are the owner's requirements. Each one is guarded by a test in `core/vision_test.go`
(plus the detailed tests next to it), and `build.sh` refuses to build if any of them fails.
Change a vision test only when the owner has asked for that behaviour to change.

| # | Requirement | Test |
|---|---|---|
| 1 | Nothing changes in the League client unless the player clicks Apply; watching champ select, Clash scouting and the setup check only read. | `TestVisionReadsNeverWrite`, `TestHealthAllGood` |
| 2 | The only things ever written to the client are your rune page and your summoner spells. | `TestVisionOnlyRunesAndSpellsAreWritten` |
| 3 | The name "lømsland" (or the app's name) never appears in the client: rune pages are named like "Ahri Mid". | `TestVisionNoAppNameInTheClient` |
| 3b | The panel beside the client doesn't show the name either. | `TestVisionNoAppNameInThePanel` |
| 3c | The app has a neutral name (Champ Select Companion): no "lømsland" in the tray, messages, window titles, errors or file and folder names. | `TestVisionNeutralName` |
| 4 | Your own rune pages are never changed unless you pick one to replace; with every slot full it asks first. | `TestVisionYourRunePagesAreSafe` |
| 5 | Clash mode (scouting) only when the client says the game is Clash; the panel only on Summoner's Rift. | `TestVisionClashOnlyInClash` |
| 6 | The numbers are EUW Emerald+ from build.lømsland.no, and the panel says so. | `TestVisionEUWFromTheSite` |
| 7 | Scouting only reads the team's Clash helper; it never saves, deletes or sends the team password. | `TestVisionClashHelperReadOnly` |
| 8 | Updates only install when signed with the owner's key, checked before anything is written. | `TestVisionUpdatesNeedTheOwnersSignature` |
| 9 | The panel can't be made to run code from the data it shows (security policy, no inline handlers, nothing loaded from outside but Riot's images). | `TestVisionPanelIsLockedDown` |
| 10 | Apply keeps each summoner spell on the key it's on (Flash on F stays on F). | `TestVisionSpellsKeepTheirKeys` |

Also decided by the owner, covered by the regular tests: the panel suggests two rune pages and two spell pairs and
the player picks; picks are remembered per champion and role; lane matchup; the app is for the Clash team on Windows.
