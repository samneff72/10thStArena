# Oakland lab

Site record for one deployment of this project. Everything here is specific to this
field — the README describes the product, this describes how one instance of it is wired.

Copied from [richmond.md](richmond.md). The addresses are identical to Richmond's and are
not a choice — see [Addressing](#addressing). What differs between fields is the station
count, the hardware, and how the e-stops are wired; those are marked **TBD** where this
site has not settled them yet, to be filled in during bring-up rather than rediscovered.

## Field

| | |
|---|---|
| Stations | **TBD** — how many driver stations are physically wired |
| Management subnet | `10.0.100.0/24`, on VLAN 1 — the same as every field; see [Addressing](#addressing) |
| Team networks | The switch routes and serves DHCP; bioarena configures it |

Any station without a driver station needs **BYP** checked in Match Play, since bioarena
always thinks in six stations.

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
| Red e-stop panel | `10.0.100.11` (**TBD** — installed?) |
| Blue e-stop panel | `10.0.100.12` (**TBD** — installed?) |
| Art-Net node | `10.0.100.100` (**TBD** — installed?) |

## Hardware

**Field e-stop — wired to the controller's own GPIO**, not to a panel Pi.

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

**Switch — Catalyst 3560-CX**, **TBD** whether this site's unit has PoE. Richmond's does
not, so its access point runs off an injector; check with `show power inline` before
assuming. Hostname `bioSwitch` unless overridden at bootstrap.

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
in station order and shuts and reopens each one around a VLAN change, which is what makes a
laptop re-request an address on its new subnet. Wire a station elsewhere and it keeps the
previous match's address.

Gi0/7 and Gi0/8 are interchangeable — both are configured identically as trunks carrying
VLANs 1 and 10–60, and nothing in the code distinguishes them.

Nothing here is configured by hand. [docs/switch-bootstrap.py](../switch-bootstrap.py)
brings the switch to the point bioarena can reach it, and bioarena pushes the VLANs, ports,
portfast and routing on its first configuration.
[switch_config.txt](../../switch_config.txt) records what the result looks like.

**Access point — Vivid-Hosting VH-113.** **TBD** whether this unit wants an API token;
Richmond's practice firmware exposes none, so its **AP Password** is blank.

## Notes

Driver station laptops take their addresses from the switch. Plugging one into any station
port registers its team there: it gets a staging address, its driver station announces the
team number, and bioarena rebuilds that station onto the team's own subnet.

A laptop used for both driving and development ends up on the field and on WiFi at once,
and the field's DHCP hands out a default route to a network with no way off it. Raise the
metric on its wired adapter or unplug it before expecting the internet to work.

This field is its own island. Nothing here is reachable from Richmond and nothing at
Richmond is reachable from here, because both use the same addresses — see
[Addressing](#addressing).
