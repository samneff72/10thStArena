# Oakland lab

Site record for one deployment of this project. Everything here is specific to this
field — the README describes the product, this describes how one instance of it is wired.

Copied from [richmond.md](richmond.md). The addresses are identical to Richmond's and are
not a choice — see [Addressing](#addressing). What differs between fields is the station
count, the hardware, and how the e-stops are wired.

## Field

| | |
|---|---|
| Stations | 6 — all of R1, R2, R3, B1, B2, B3 |
| Management subnet | `10.0.100.0/24`, on VLAN 1 — the same as every field; see [Addressing](#addressing) |
| Team networks | The switch routes and serves DHCP; bioarena configures it |

Every station is wired, so nothing needs **BYP** checked in Match Play. Richmond does, with
only four; this field does not.

## Addressing

**Identical to Richmond, deliberately.** Every field uses `10.0.100.0/24` — it is not a
per-site choice. Driver station software is hardcoded to find its FMS at `10.0.100.5`, and
bioarena matches it: `ServerIpAddress` in [network/switch.go](../../network/switch.go) is
fixed at that address and the driver station listener binds to it. A field addressed
anywhere else has a listener that never starts.

**Oakland and Richmond therefore cannot share a network.** Both claim `10.0.100.5`, so on
one LAN or one VPN neither would work. The two fields are separate islands with no route
between them, and reaching both from one machine means reaching them one at a time — over a
bench network, or by plugging into the field you want.

Nothing in this section is a decision this site gets to make; it is recorded so that the
next person does not try to renumber Oakland to keep the two reachable at once.

## Addresses

| Device | Address |
|---|---|
| Field controller Pi | `10.0.100.5` |
| Switch (management) | `10.0.100.3` — set as **Switch Address** under Arena → Settings, with its Telnet password |
| Field access point | `10.0.100.2` — set as **AP Address** under Arena → Settings. Backup `192.168.69.1`, where it turns up after a reset; the Pi and switch carry addresses in that subnet so the fallback stays reachable |
| Red e-stop panel | `10.0.100.11` — planned, not yet installed |
| Blue e-stop panel | `10.0.100.12` — planned, not yet installed |
| Art-Net node | `10.0.100.100` — **TBD**, Hub LEDs not decided |

## Hardware

**Field e-stop — wired to the controller's own GPIO.** Separate from the alliance panels
below: this is the field-wide stop, and it stays on the controller even once the panels are
in. The panels handle per-station stops.

| Contact | BCM pin | Setting |
|---|---|---|
| NO | 24 | **Field E-Stop NO GPIO Pin** under Setup → Settings |
| NC | 25 | **Field E-Stop NC GPIO Pin** under Setup → Settings |

Both are entered as BCM numbers. Getting them the wrong way round does not report a fault —
a released button then decodes as `nc=1, no=0`, which is a *stop*, so the field sits stopped
with no error message. Released, the healthy reading is NC low and NO high.

**These two pins read misleadingly by hand.** BCM 9–27 power up with an internal
pull-*down*, so `pinctrl get 24,25` on a Pi where bioarena has not claimed them reads `0`
whatever is connected — which looks exactly like a pressed button. Bioarena requests both
lines with an explicit pull-up, so this only affects readings taken before it opens them, or
while the service is stopped. To check by hand the way bioarena will see them:

```bash
gpioget -B pull-up gpiochip0 24 25
```

Released and healthy, that prints `0 1` — NC closed to ground, NO held up by the pull-up.
Pressed, `1 0`. Anything else is the wiring-fault pair. Richmond uses 5 and 6 instead, which
default to pull-up and so read the same either way; Oakland's do not.

The service account needs the `gpio` group or it cannot open `/dev/gpiochip0` at all.
`deploy-fms.sh` handles that, but group membership is taken at process start, so adding it
by hand needs a restart rather than a settings save.

**E-stop panels — planned, not yet installed.** One Pi per alliance, red at `10.0.100.11`
and blue at `10.0.100.12`, each powered over PoE from the switch via a PoE HAT. Deploy with
`./deploy-panel.sh <bench-address> red`, where the argument is where the panel answers from
your build machine and the alliance is what writes its static field address into the service
file. Wiring and pin assignments are in
[docs/hardware-wiring.md](../hardware-wiring.md); once installed, record this field's
actual pin map here, since it need not match that document's example.

**Hub LEDs — undecided.** If a DMX gateway is added it goes on Gi0/9 at `10.0.100.100`,
inside the switch's excluded `.1`–`.125` range so a static there can never collide with a
lease. Record the model and whether it speaks Art-Net or sACN, since that drives the
checkbox under Arena → Settings → LEDs.

**Switch — Catalyst 3560-CX**, PoE-capable, hostname `bioSwitch` unless overridden at
bootstrap. Richmond's is a non-PoE SKU, so this is the one piece of hardware the two fields
genuinely differ on.

PoE here powers the **e-stop panel Pis** and nothing else. A Pi has no native PoE, so each
panel needs a PoE HAT. PoE is on by default as `power inline auto`, so the ports need no
configuration — but check the budget with `show power inline` before adding loads, since an
8-port unit has little of it and a device that exceeds what is left simply never comes up,
with no obvious error.

**The access point is the exception and must not be powered from the switch** — see the
access point section below.

| Port | Role | Mode |
|------|------|------|
| Gi0/1 | R1 driver station | access vlan 10 |
| Gi0/2 | R2 driver station | access vlan 20 |
| Gi0/3 | R3 driver station | access vlan 30 |
| Gi0/4 | B1 driver station | access vlan 40 |
| Gi0/5 | B2 driver station | access vlan 50 |
| Gi0/6 | B3 driver station | access vlan 60 |
| Gi0/7 | Field controller Pi | trunk, native vlan 1 |
| Gi0/8 | VH-113 AP | trunk, native vlan 1 |
| Gi0/9 | Art-Net node | access vlan 1 |
| Gi0/10–12 | spare, for troubleshooting | access vlan 1 |

Gi0/1–6 are not a site choice. [`dsPortInterfaces`](../../network/switch.go) hardcodes them
in station order, and the baseline builds each one as that station's access port. Wire a
station elsewhere and its laptop lands on the wrong team's VLAN.

Gi0/7 and Gi0/8 are interchangeable — both are configured identically as trunks carrying
VLANs 1 and 10–60, and nothing in the code distinguishes them.

Nothing here is configured by hand. [docs/switch-bootstrap.py](../switch-bootstrap.py)
brings the switch to the point bioarena can reach it, and bioarena pushes the VLANs, ports,
portfast and routing on its first configuration.
[switch_config.txt](../../switch_config.txt) records what the result looks like.

**Access point — Vivid-Hosting VH-113.** No API token; **AP Password** under
Arena → Settings is blank, the same as Richmond. Confirm with
`curl -s http://10.0.100.2/status` from the Pi — JSON back means leave it empty, a `401`
means this build wants one after all.

> **The AP takes passive 12 V from its own adapter. Do not let the switch power it.**
> This is the one place Oakland's PoE switch is a hazard rather than a convenience. The
> VH-113 is not an 802.3af/at device: it does not negotiate, and its injector puts 12 V on
> the line unconditionally. Two separate risks follow — the switch sourcing PoE into a
> device that never asked for it, and the passive injector feeding 12 V back toward a
> switch port that is expecting to be the power source.
>
> Belt and braces, disable PoE on that port explicitly:
>
> ```
> configure terminal
> interface GigabitEthernet0/8
> power inline never
> end
> ```
>
> Then `write memory`. Bioarena's baseline does not set this — it never emits a
> `power inline` command — so it is a manual step that survives on the switch's saved
> configuration. Re-check it after any `write erase`.

## Notes

Driver station laptops take their addresses from the switch, once their station has a team
registered. Registration is by hand: type the team numbers into Match Play and press
Register. Plugging a laptop into a station with no team registered does nothing — the
station has no subnet, so the laptop gets no address and never connects.

The card shows **CABLE** when a station has something plugged in and no team registered, and
**No cable** when a team is registered and nothing is plugged in. Both come from the
switch's own port link state, which is the only thing that sees a station with no subnet.

Entering a team number looks the team up: a known team fills in its WPA key, and an unknown
one offers to add it to the database on the spot.

A laptop used for both driving and development ends up on the field and on WiFi at once,
and the field's DHCP hands out a default route to a network with no way off it. Raise the
metric on its wired adapter or unplug it before expecting the internet to work.

This field is its own island. Nothing here is reachable from Richmond and nothing at
Richmond is reachable from here, because both use the same addresses — see
[Addressing](#addressing).
