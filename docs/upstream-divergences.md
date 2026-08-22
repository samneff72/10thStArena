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
unrelated fork changes — driver station port cycling, incremental configuration,
`GetStationForTeamId`, `ServerIpAddress` as a var rather than a const, `DevMode` removed,
the module path. A PR should touch only `runConfigCommand` and add the two tests. Check
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
| `field/team_match_log.go` | Logs written to `logs/` rather than `static/logs/`. | Upstream's placement makes them downloadable for free, but it also means the deploy step's `scp -r static` copies every accumulated log to the field Pi. bioarena serves them from `/logs/` instead. |
| `web/web.go` | Serves `/logs/` with directory listing and no authentication. | Restores the browse-and-download behaviour lost by moving the directory, deliberately matching how `/static/` is served. |
| `field/`, `hardware/`, `cmd/estop-panel` | GPIO and network e-stop panels, free practice mode, half-field conveniences. | Practice-field hardware and workflow with no upstream counterpart. |
| `network/switch.go` | Bioarena owns the switch configuration outright: on the first configuration of a run it pushes the VLAN database, the station and trunk ports, portfast and `ip routing`, and saves it, so setting up a field is wiring plus a console bootstrap script rather than hand-written IOS. Unregistered stations get a staging subnet (`172.16.<vlan>.0/24`) routed to the FMS, and a driver station connecting from one is registered to the station whose port it is plugged into, clearing that team from any station it previously occupied. Configuration is incremental: the last applied teams are cached, only the stations whose team changed are rebuilt, and only their driver station ports are cycled. Those ports are fixed in code as `GigabitEthernet0/1`–`0/6` in station order, so a 3560-CX wired that way is assumed. Full rebuild whenever the switch's state is unknown — at startup and after a failure. `NewSwitch` takes a `SwitchConfig` struct. | Upstream leaves an unregistered station without a subnet, so a laptop in the wrong port gets no address, never connects, and produces silence -- and its auto-assignment guesses the station from the ARP table, which cannot see a laptop that has no address. Upstream also reconfigures only between matches, where rebuilding everything costs nothing. This fork also reconfigures on free practice slot changes, where the other stations have robots being driven. The ports were briefly an operator-editable command block, which let an invalid interface range be entered and then fail silently — a Telnet read timeout counts as success — so fixing the wiring removes the setting and the mistake together. |

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
