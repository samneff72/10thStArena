# Divergences from upstream cheesy-arena

bioarena is a fork of [Team254/cheesy-arena](https://github.com/Team254/cheesy-arena).
This is the register of where the two differ deliberately, and which differences are
worth sending back upstream.

Keeping this current matters because some of the fork's value depends on staying close
to upstream — in particular `led/` is byte-identical, so upstream lighting changes can be
taken with a checkout rather than a merge.

To add the upstream remote if it is not configured:

```bash
git remote add upstream https://github.com/Team254/cheesy-arena.git
git fetch upstream main
```

To check a file or directory:

```bash
git diff upstream/main -- led/
```

---

## Upstream candidates

Changes worth offering back. None are blocking for upstream; they are hardening or
generalisation rather than fixes for bugs upstream can currently hit.

### `network/switch.go` — terminate the command before appending `end`

**Status:** ready to propose. Not yet submitted.

`runConfigCommand` wraps the caller's text as `config terminal\n<command>end\n`. If the
caller's last line is not newline-terminated, `end` is concatenated onto it — IOS
receives something like `no shutdownend`, rejects it, and the whole configuration block
silently fails to close. The failure is invisible because a Telnet read timeout is
treated as success further up.

**Upstream cannot currently trigger it.** Both upstream callers build strings ending in
`\n`, and upstream does not cycle driver station ports at all. bioarena hit it by adding a
third caller whose text did not honour the unstated precondition.

**The pitch is therefore hardening, not a bug report.** A helper that silently corrupts
its input when a precondition is unmet is worth guarding regardless of whether any
current caller violates it — particularly one whose failure mode is a silently discarded
configuration block on live field hardware.

The change is three lines and is a no-op for newline-terminated input, so upstream's
existing callers and their tests are unaffected:

```go
if command != "" && !strings.HasSuffix(command, "\n") {
    command += "\n"
}
```

Covered by `TestConfigCommandTerminatesLastLine` and
`TestConfigCommandDoesNotDoubleTerminate` in `network/switch_test.go`.

**Submit it as an isolated patch, not a file copy.** `network/switch.go` carries several
unrelated fork changes — the standing baseline, incremental configuration, the status badge,
`ServerIpAddress` as a var rather than a const, `DevMode` removed, the module path. A PR should touch only `runConfigCommand` and add the two tests. Check
what else has drifted before branching:

```bash
git diff upstream/main -- network/switch.go
```

Note also that upstream's tests construct the switch as `NewSwitch(address, password)`,
where bioarena's takes a `SwitchConfig`. The upstream-facing tests must use upstream's
two-argument signature.

---

## Fork-local

Deliberate, specific to a practice field, and not appropriate to send upstream.

| Area | Divergence | Why it stays local |
|---|---|---|
| `game/hub.go` | Ported from upstream with the scoring half omitted — `ShiftCounts`, `UpdateState`, `GetShiftCount`, `GetTeleopActiveFuelCount`, `getCurrentShift`, `getScoringGracePeriod`. `IsShiftActive` exported. | bioarena does not score. The export is needed because the lighting drivers live outside `game/`. |
| `game/match_timing.go` | Adds `WarmupDurationSec` and keeps a flat `TeleopDurationSec` alongside upstream's shift constants. | Warmup is a practice-field feature with no upstream counterpart, threaded through the match state machine. |
| `led/override.go` | New file: practice-field fixture layout and capability fallback. | Upstream fields have the full 16-fixture hardware. Deliberately additive so the six ported `led/` files stay byte-identical. |
| `led/artnet.go` | New file: `ArtNetController`, an Art-Net alternative to the E1.31 sACN in `controller.go`, chosen by a checkbox under Arena → Settings → LEDs. Embeds `Controller` and replaces only `SetAddress` and `Update`, reusing the zone rendering and fixture layout. | Upstream fields have sACN nodes. A practice field buys whatever DMX gateway is cheap, and plenty of them speak only Art-Net. Additive for the same reason as `override.go`: a protocol switch inside `controller.go` would end the byte-identical property. |
| `web/match_play.go`, `templates/match_play.html`, `static/js/match_play.js`, `field/arena.go` | Match sounds play on the Match Play page. The websocket subscribes to `PlaySoundNotifier`, the template carries the `<audio>` elements, and `MuteMatchSounds` defaults to true. | Upstream plays cues on the audience display. This fork has no `audience_display` template — only `static/js/audience_display.js` survives, orphaned — so the notifier fired into nothing and the field was silent regardless of settings. The operator's page is the only one there is. Muted by default because a practice field is usually a room with people in it. |
| `network/switch.go`, `field/arena.go`, `field/driver_station_connection.go` | **Removed, was fork-local.** Staging subnets (`172.16.<vlan>.0/24`) for unregistered stations, self-registration from them, ARP-table station detection (`GetStationForTeamId`), driver station port cycling (`CycleStationPort`, `recoverMissingDriverStations`) and the `AutoConfigureTeams` setting are all gone. Teams are registered by typing numbers into Match Play, as upstream does. | The auto-registration was slower than it replaced: every driver station arrival cost a switch round trip and a port cycle, and a wrong guess cost more to undo than typing six numbers. What is kept is the read-only port link poll, which is the only thing that sees a cable in an unregistered station — it runs on the periodic task, not the match-load path. Upstream's own wrong-station check covers a laptop in the wrong port. |
| `game/match_sounds.go`, `field/arena.go` | `shift_change` is not scheduled in `MatchSounds`. It is sounded from the lighting transition in `Arena.Run`, gated by `shiftChangeSounds` to the four HUB handovers. | Upstream schedules the cue at four timestamps computed from the shift constants, which is safe there because its lighting reads the same constants. Bioarena derives shift boundaries through `teleopShift` from a flat `TeleopDurationSec`, so a second schedule could disagree with the lights. Driving both from one computed transition means the cue and the HUB can never drift apart. |
| `templates/match_play.html`, `static/js/match_play.js`, `web/match_play.go`, `web/setup_teams.go`, `field/arena.go` | Match Play carries a WPA key field per station alongside the team number, stacked in the width the number alone used to have. Entering a team number looks it up: a known team fills in its key, an unknown one offers to add it to the database. Register stores any changed keys before substituting, so the access point push already triggered carries them; a blank key leaves the team's existing one, and a key the operator typed is never overwritten by a later lookup. `quick-add` gives a new team the zero-padded team number as its key. | Upstream sets keys from the team list, which assumes an event where teams are entered ahead of time. A practice field registers whoever turns up, and leaving Match Play for Setup → Teams or the Free Practice page to set a key was the reason matches got set up on the wrong page. The default key matters because a team created without one has no working WiFi, and nothing says so until a robot fails to associate. The same lookup and modal are Free Practice's, reused rather than reimplemented. |
| `field/team_match_log.go` | Logs written to `logs/` rather than `static/logs/`. | Upstream's placement makes them downloadable for free, but it also means the deploy step's `scp -r static` copies every accumulated log to the field Pi. bioarena serves them from `/logs/` instead. |
| `web/web.go` | Serves `/logs/` with directory listing and no authentication. | Restores the browse-and-download behaviour lost by moving the directory, deliberately matching how `/static/` is served. |
| `field/`, `hardware/`, `cmd/estop-panel` | GPIO and network e-stop panels, free practice mode, half-field conveniences. | Practice-field hardware and workflow with no upstream counterpart. |
| `network/switch.go` | Bioarena pushes the switch's standing configuration itself: on the first configuration of a run it applies the VLAN database, the station and trunk ports, the DMX gateway port, portfast, `ip routing` and `ntp server`, and saves it — so setting up a field is wiring plus a console bootstrap script rather than hand-written IOS. Per-match configuration is incremental: the last applied teams are cached and only the stations whose team changed are rebuilt, with a full rebuild whenever the switch's state is unknown, at startup and after a failure. Driver station ports are fixed in code as `GigabitEthernet0/1`–`0/6` in station order. `NewSwitch` takes a `SwitchConfig` struct. | Upstream assumes the switch was configured by hand from `switch_config.txt`; the baseline is what makes a new field wiring plus a script, and it costs no match time because it runs once per process start. Incremental rebuild exists because this fork also reconfigures on free practice slot changes, where the other stations have robots being driven and a full rebuild would drop them. |

---

## Kept byte-identical

`led/` — all six ported files (`color.go`, `controller.go`, `controller_test.go`,
`fixture.go`, `mode.go`, `zone.go`). Local behaviour goes in `led/override.go` instead.

```bash
git diff upstream/main -- led/color.go led/controller.go led/controller_test.go \
  led/fixture.go led/mode.go led/zone.go
```

Empty output means the property still holds. If it ever stops being empty, either revert
the edit into `override.go` or update this document to say the property was given up and
why.
