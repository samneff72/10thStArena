# Richmond lab

Site record for one deployment of this project. Everything here is specific to this
field — the README describes the product, this describes how one instance of it is wired.

Copy this file when standing up another site and change the values.

## Field

| | |
|---|---|
| Stations | 4 — R1, R2, R3, B1 |
| Management subnet | `10.0.100.0/24`, on VLAN 1 |
| Team networks | The switch routes and serves DHCP; bioarena configures it |

B2 and B3 need **BYP** checked in Match Play, since bioarena always thinks in six
stations.

## Addresses

| Device | Address |
|---|---|
| Field controller Pi | `10.0.100.5` |
| Switch (management) | `10.0.100.3` — set as **Switch Address** under Arena → Settings, with its Telnet password |
| Field access point | `10.0.100.2` — set as **AP Address** under Arena → Settings. Backup `192.168.69.1`, where it turns up after a reset; the Pi and switch carry addresses in that subnet so the fallback stays reachable |
| Red e-stop panel | `10.0.100.11` (not installed) |
| Blue e-stop panel | `10.0.100.12` (not installed) |

## Hardware

**Switch — Catalyst 3560-CX**, IP Base, hostname `bioSwitch`, management `10.0.100.3`
on VLAN 1.
Boot image lives in `flash:c3560cx-universalk9-mz.152-7.E/`, with `boot system` set so it
does not stop at the boot loader.

Both Gi0/7 and Gi0/8 are trunks. The VH-113 tags each team's SSID onto that team's VLAN, so
VLANs 10–60 have to reach it — an access port there leaves every robot associated to WiFi
and unable to reach anything. The Pi takes its management address untagged on the native
VLAN, so a trunk serves it identically to an access port and both ends match.

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

Clearing a match bounces the link on every station that has a team registered, which is what
makes its driver station laptop ask for an address again — it drops the one it had when the
match ended. Nothing else cycles these ports.

B2 and B3 have ports and VLANs but no driver stations yet; a station with no team gets no
subnet, so VLANs 50 and 60 stay dark until one is registered.

Nothing here is configured by hand. [docs/switch-bootstrap.py](../switch-bootstrap.py)
brings the switch to the point bioarena can reach it, and bioarena pushes the VLANs, ports,
portfast and routing on its first configuration.
[switch_config.txt](../../switch_config.txt) records what the result looks like.

**Access point — Vivid-Hosting VH-113.** No API password; the practice firmware exposes no
token, so **AP Password** under Arena → Settings is blank.

**Art-Net node — PKNight CR011R**, static `10.0.100.100`, on Gi0/9. The switch's
`dhcppool` covers `10.0.100.0/24` but excludes `.1`–`.125`, so a static in that range can
never collide with a lease. Its address goes in **sACN Receiver Address** under
Arena → Settings → LEDs.

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
