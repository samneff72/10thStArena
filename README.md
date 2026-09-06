# Practice Field Controller

A Raspberry Pi service for running FRC practice sessions. Controls up to 6 robots across red and blue alliances. Runs timed auto and teleop periods. Manages the field access point and VLAN isolation automatically. Accessible from any browser on the field network.

## Requirements

- Raspberry Pi 4 running 64-bit Raspberry Pi OS (Trixie or Bookworm). 32-bit images work
  too — see [Raspberry Pi OS releases](#raspberry-pi-os-releases) for the one build flag
  that changes
- [Go 1.23+](https://golang.org/dl/) on your build machine
- Vivid-Hosting VH-113 field access point (running OpenWRT)
- Vivid-Hosting VH-109 radio on each robot
- Catalyst 3560-CX switch, IP Base, wired as [Step 2](#step-2--configure-the-managed-switch) describes
- Static IP assigned to Pi (recommend `10.0.100.5`)

## Install

### Preparing a fresh Pi image

Flash with Raspberry Pi Imager and open its customisation panel (the gear icon, or
`Ctrl+Shift+X`) **before writing**. Several things are far easier to set there than after
first boot.

- **Name the login user whatever you like.** Bookworm and later no longer create `pi` by
  default, and nothing here depends on the name: bioarena installs to `/opt/bioarena` and
  runs as a dedicated `bioarena` system account. The `scp` commands below use `<USER>` for
  whichever account you log in with.
- **Enable SSH.** A fresh image has it off, so without this the first boot needs a
  keyboard and monitor.
- **Set the keyboard layout and locale.** A mismatched layout is why keys like `|` end up
  missing when you are working at the Pi directly.
- **Set the WiFi credentials** if the Pi needs to reach the internet before it joins the
  field network. It does — chrony has to be installed while it still has a route out.

After first boot, in this order:

1. **Install the packages, while the Pi still has internet.** This is the step that is
   painful to go back for: once the Pi is on the field network there is no route out, and
   an end-of-life release compounds it by needing its apt sources repointed at
   `archive.debian.org` first.

   ```bash
   sudo apt install chrony
   ```

   Chrony on every field — it makes the Pi the field's time source, without which the
   switch and the controller timestamp their logs years apart. See
   [Step 5](#step-5--serve-time-from-the-pi).

2. Set the static field address — see [Step 1](#step-1--assign-a-static-ip-to-the-pi).
3. Install the chrony drop-in and point the switch at the Pi, per
   [Step 5](#step-5--serve-time-from-the-pi).
4. Copy the binary, assets, and service file (below), plus `config.yaml`. Carry `event.db`
   across too if you want the previous field's settings, teams, and admin password;
   without it the first start creates a fresh database with the default password.
5. Confirm the GPIO chip name with `gpiodetect` if you use e-stop panels or GPIO lights.

Two things that will look like faults and are not:

**SSH host key mismatch.** A reimaged Pi generates new host keys, so `ssh` and `scp` refuse
to connect at the same address. Clear the stale entry:

```bash
ssh-keygen -R 10.0.100.5
```

Addresses are tracked separately, so repeat it for whatever address you used before the Pi
moved onto the field network.

**Wrong timestamps in the logs.** The Pi has no real-time clock. On a field with no route
to the internet it restores the last known time from `fake-hwclock` at boot, so journal
entries and match logs can be days behind until it next sees an NTP server. See
[Step 5](#step-5--serve-time-from-the-pi) for making the field's clocks at least agree with
each other.

**Build the Pi binary**

Run this on your development machine (not on the Pi):

```bash
./build-pi.sh
```

This cross-compiles two ARM binaries: `bioarena-pi` for the field controller and
`estop-panel-pi` for the e-stop panel Pis. The `-pi` suffix keeps them from shadowing a
local build. Running the script also prints the full deploy sequence for both.

It targets 64-bit (`aarch64`) by default. On a 32-bit image, build with:

```bash
ARCH=arm GOARM=7 ./build-pi.sh
```

Check which you need with `uname -m` on the Pi: `aarch64` for the default, `armv7l` for
the override. The wrong one does not fail informatively — the Pi reports
`cannot execute binary file: Exec format error`.

**Deploy**

```bash
./deploy-fms.sh 192.168.1.42
```

That is the whole thing. It builds, creates the `bioarena` service account and
`/opt/bioarena` if this Pi has never been deployed to, copies the binary and the web
assets, installs and enables the service, starts it, and checks it stayed up. Nothing is
optional and nothing is remembered between runs, because a deploy that needs you to
remember a flag eventually goes out missing a file.

Safe to run repeatedly, and safe on a Pi that has never seen bioarena, so "run it again" is
always a reasonable answer. A failed step stops the run and names itself, leaving the Pi on
whatever it was running before; a service that starts and then dies prints its last twenty
log lines.

**That address is an example, and it is not the Pi's field address.** Pass whatever the Pi
answers on from your build machine right now — normally a bench or workshop network the two
share. `10.0.100.5` is what `bioarena.service` gives the Pi on `eth0` once it is wired into
the field, so driver stations can find it; it is not a route your laptop has, and your build
machine has no reason to join the field network at all.

Finding a Pi on the bench network, easiest first:

```bash
ssh admin@raspberrypi.local
```

That is mDNS, using the hostname set when the card was flashed — worth setting per Pi, since
two of them answering to `raspberrypi.local` is a coin flip over which one you just deployed
to. Otherwise run `hostname -I` on the Pi if it has a screen, or read the bench router's
client list.

Add your login user if it is not `admin`:

```bash
./deploy-fms.sh 192.168.1.42 sam
```

Panels take the alliance as well — see [Field hardware](#field-hardware):

```bash
./deploy-panel.sh 192.168.1.43 red
```

The two arguments are unrelated addresses: the first is where the panel answers now, the
alliance is what writes its static field address (`10.0.100.11` red, `10.0.100.12` blue)
into the service file.

The service runs as a system account with no login, so a field controller does not depend
on which username the SD card was flashed with. It installs to `/opt/bioarena` and writes
`event.db`, `logs/` and `db/backups/` there.

**The display opens the field automatically.** The deploy installs a kiosk autostart entry
for your login user, so a Pi with a monitor logs in, waits for the service to answer, and
opens `http://localhost:8080` full screen. It waits rather than racing: the desktop session
comes up before bioarena has finished configuring the switch, and a browser that opened
first would sit on a connection error.

Needs a desktop image and Chromium (`sudo apt install chromium`), and auto-login enabled in
`raspi-config` if you want it without a keyboard. On a headless Pi the entry is simply never
read. Turn it off by deleting `~/.config/autostart/bioarena-kiosk.desktop`.

**E-stop panel Pis get the same screen.** `deploy-panel.sh` installs the identical kiosk,
pointed at the controller — `http://10.0.100.5:8080` — rather than at itself, since a panel
runs `estop-panel` on 8765 and has no web UI of its own. A panel with a monitor beside the
alliance station therefore shows whatever the operator is running, and follows along when
the operating page changes. Installed unconditionally: on a headless panel the autostart
entry is never read, so there is nothing to configure either way.

A panel that boots before the controller waits for it rather than showing an error, which
matters more here than on the controller itself — it is waiting on another machine that may
not be powered yet.

**Set up key authentication first**, or every deploy asks for a password several times. On
your development machine:

```bash
ssh-keygen -t ed25519 -C "bioarena deploy"
```

```bash
ssh-copy-id <USER>@10.0.100.5
```

On Windows there is no `ssh-copy-id`, so from Git Bash:

```bash
cat ~/.ssh/id_ed25519.pub | ssh <USER>@10.0.100.5 "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
```

After that a deploy is one command and no prompts.

**What the service does at startup**

`bioarena.service` is installed by the deploy script, so there is nothing to move by hand.
Confirm what is installed with `systemctl cat bioarena`. Before starting the binary it:

- assigns `10.0.100.5/24` to `eth0` — the address driver stations are hardcoded to look for
- assigns `192.168.69.5/24` as well, so the access point stays reachable on its backup address
- routes `10.0.0.0/8` and `172.16.0.0/12` via the switch, so replies to driver stations on
  team and staging subnets have a way back

Each is prefixed `-` in the unit, so a restart with the addresses already present carries
on rather than failing.

**Open the web UI**

```
http://10.0.100.5:8080
```

## Network Setup

This section is the most important part of the physical field setup. Read it carefully before powering anything on.

### Why this network layout matters

FRC Driver Station software is hardcoded to contact its FMS at `10.0.100.5` on ports `1750` (TCP) and `1121`/`1160` (UDP). The Pi must live at that address on the wired field network. Each team lives on its own team-number-derived subnet isolated by a VLAN, with the driver station laptop wired into that VLAN and the robot joining it over WiFi. The access point handles the wireless side; the switch enforces isolation and routes each team subnet to the FMS.

### Topology

This mirrors a competition field: **driver station laptops are wired**, **robots are
wireless** through their own radios.

```
                        Catalyst 3560-CX
   ┌──────────────────────────────────────────────────────────────┐
   │  Gi0/1-6                          Gi0/7          Gi0/8       │
   │  access, VLAN 10..60              trunk          trunk       │
   └─────┬───────────────────────────────┬──────────────┬─────────┘
         │ one per station               │              │
         │                               │              │
 ┌───────┴────────────┐      ┌───────────┴──────┐  ┌────┴──────────────┐
 │ DS laptops         │      │ Raspberry Pi 4   │  │ VH-113 AP         │
 │ one per station    │      │ FMS 10.0.100.5   │  │ 10.0.100.2        │
 │ 10.TE.AM.20-.199   │      │ and 192.168.69.5 │  └────┬──────────────┘
 │ (DHCP from switch) │      └──────────────────┘       │
 └────────────────────┘                                 │ WiFi, one SSID
                                                        │ per team
                                              ┌─────────┴────────┐
                                              │ Robot radios     │
                                              │ VH-109           │
                                              │ 10.TE.AM.x       │
                                              └──────────────────┘
```

Each station's laptop and its robot land on the same VLAN and team subnet — the laptop
over its wired port, the robot over WiFi. Sharing a subnet is what lets the driver
station find the roboRIO by mDNS, which does not cross subnet boundaries.

The switch routes between those team subnets and the FMS, and serves each of them a DHCP
scope, so a laptop needs no configuration at all: plug it in, and it is addressed and
registered to whichever station it is plugged into.

### Step 1 — Assign a static IP to the Pi

The Pi must have `10.0.100.5` on the interface connected to the switch. The systemd service handles this automatically via:

```
ExecStartPre=/sbin/ip addr add 10.0.100.5/24 dev eth0
```

For a permanent static IP that survives reboots without the service, the method depends on
the OS release.

**Trixie and Bookworm** use NetworkManager. Name the connection bound to the field NIC —
`nmcli connection show` lists them, typically `Wired connection 1`:

```bash
sudo nmcli connection modify "Wired connection 1" ipv4.method manual ipv4.addresses 10.0.100.5/24
```

```bash
sudo nmcli connection up "Wired connection 1"
```

Leave the gateway and DNS unset. The field network has no route off itself, and a default
route pointing into it would send the Pi's own internet traffic nowhere.

**Buster and other dhcpcd releases** edit `/etc/dhcpcd.conf` instead:

```
interface eth0
static ip_address=10.0.100.5/24
```

Do not put the Pi on a robot subnet (`10.TE.AM.x`). Use a dedicated management subnet such as `10.0.100.0/24`.

### Step 2 — Configure the managed switch

**Nobody configures this switch by hand.** Bioarena owns its configuration: the VLAN
database, the station and trunk ports, portfast, `ip routing`, and the per-match VLANs and
DHCP pools. What a person does is wire it and run one script.

**Switch requirements.** Bioarena issues six concurrent SVIs with IP addresses and six
DHCP pools, so the switch must be Layer 3 capable with the IOS DHCP server — a
3550/3560/3750-class unit, IP Base rather than LAN Base. A 2960 will not work: it allows
only one active SVI, and the LAN Lite images have no DHCP server. Check with
`show version` before wiring a field around one.

**Wire it like this.** These are the only decisions left to a person, because the code
assumes them:

| Port | Role |
|------|------|
| `GigabitEthernet0/1`–`0/6` | Driver stations, in station order: R1, R2, R3, B1, B2, B3 |
| `GigabitEthernet0/7` | Raspberry Pi |
| `GigabitEthernet0/8` | Vivid-Hosting VH-113 access point |

**Then bootstrap it over the console.** A switch out of the box has no address and no
password, so nothing can reach it over the network — that part is console-only by
definition. [docs/switch-bootstrap.py](docs/switch-bootstrap.py) does it without anyone
composing IOS:

`deploy-fms.sh` already put it on the Pi, so this runs there with a USB console cable
between the Pi and the switch's console port:

```bash
sudo python3 ~/switch-bootstrap.py --password <SWITCH_PASSWORD>
```

That names the switch `bioSwitch`, sets the management address (`10.0.100.3` by default),
the enable and VTY passwords to
the same value, enables Telnet, points the boot loader at the installed IOS image so the
switch stops halting at the `switch:` prompt, and saves. Run it again any time; it changes
nothing the second time.

**Then enter the address and password** under **Arena → Settings**. They are the only
switch settings in the web UI. On its first configuration bioarena pushes the standing
configuration — VLANs named per station, the six access ports with portfast, the two
trunks carrying all six VLANs, `ip routing` — and saves it, so a power cycle brings the
switch back ready to run a field on its own.

That saving happens once, before any team is on the field. Bioarena never writes the
configuration later, and neither should you: doing it mid-session bakes whichever teams
were loaded into the startup config, and their DHCP pools come back after every reboot as
state nobody put there.

[switch_config.txt](switch_config.txt) records what the result looks like, for reference or
for a switch you would rather set up by hand.

Cycling a station's port is what makes its laptop re-request an address on the new subnet
rather than keep the previous match's. Only the stations whose team changed are cycled, so
the others keep their VLAN, their addresses, and their driver station connections — which
is what makes free practice usable while other teams are driving.

The first configuration after a restart rebuilds everything, because the switch outlives
the process and may have been changed by hand in between.

#### Staging networks and self-registration

An unregistered station is not left dead. It gets a **staging subnet** — `172.16.<vlan>.0/24`,
routed to the FMS — so a laptop plugged into it still receives an address and its driver
station still connects. The driver station announces the team number configured in its own
software, and the address it connects from names the port it is in, so bioarena registers
that team to that station and rebuilds it onto the team's real subnet.

In practice: plug a driver station into any station's port and it registers itself there.
Move the cable to another port and the team moves with it, cleared from the station it
left, so a team is never registered in two places.

This is the only identification that survives driver stations being shared between teams.
It comes from the team number set in the driver station software, not from anything about
the laptop — a MAC address would name the hardware, which on a shared field says nothing
about who is driving.

`172.16/12` rather than somewhere in `10/8` because team subnets are derived from team
numbers, and a team numbered under 100 lands on `10.0.NN.0/24` — team 33 would collide with
a staging subnet keyed the obvious way. The driver station does not care what its own
address is, only that it can reach `10.0.100.5`.

Turn it off with **Auto-Configure Teams** under Arena → Settings, which leaves the staging
subnets in place but stops bioarena acting on what connects to them. Registration is also
skipped once a match is running.

**Check the license level** with `show version`. Bioarena needs six concurrent SVIs with
addresses and the IOS DHCP server. IP Base has both; verify before wiring a field on
LAN Base.

**Telnet is likely disabled.** Recent IOS images ship SSH-only, so the VTY lines need
`transport input telnet` set over the console before bioarena can reach the switch at all.

> **Bioarena will not touch the switch or the AP unless network security is enabled.**
> Off means no errors, no switch activity, nothing at all — so if the field appears
> inert, check this first.
>
> It defaults to on. `config.yaml` seeds it on a first run only; from then on the
> checkbox under **Arena → Settings** is authoritative and survives restarts. Turn it off
> there for bench testing, or when a switch fails mid-session and you want to keep
> running matches.

#### First-time switch setup via console cable

A USB-to-RJ45 console cable is required. Connect it to the switch's port labelled
`CONSOLE` — on most units it is on the rear and physically identical to the Ethernet
ports. If the switch also has a USB console socket, unplug anything in it: on many Cisco
models an occupied USB console **disables the RJ45 console** silently.

[docs/console.py](docs/console.py) opens an interactive session using only the Python standard
library, so it needs no packages. That matters on older Raspbian releases, whose apt
repositories have moved to `archive.debian.org` and can no longer install `screen` or
`minicom` without repointing the sources list.

`deploy-fms.sh` copies it to the Pi on every deploy, so it is already there:

```bash
ssh <USER>@<PI_ADDRESS>
python3 ~/console.py            # /dev/ttyUSB0 at 9600 8N1; Ctrl-] to exit
python3 ~/console.py --list     # if unsure which device the cable is
```

Cisco's own USB console ports enumerate as `/dev/ttyACM0` rather than `/dev/ttyUSB0`.

**Run it from the Pi, not from a Windows dev machine.** It drives the terminal through
`termios`, which Windows has no equivalent of, so it exits with instructions rather than
running. The Pi is on the field with the switch anyway. If you would rather stay on
Windows, PuTTY does the same job: connection type Serial, the COM port from Device
Manager, 9600 baud, 8 data bits, 1 stop bit, no parity, no flow control.

Over SSH, run it with `ssh -t` so a terminal is allocated — without that the session
attaches but shows nothing:

```bash
ssh -t <USER>@10.0.100.5 "python3 ~/console.py"
```

Press Enter a few times after connecting — the console prints nothing until it receives
input, which is the most common reason a working cable looks dead. If you get garbage
characters instead of a prompt, the cable is fine and the baud rate is wrong; try
`-b 115200`.

If you see nothing at all through a full power cycle of the switch, the problem is not
the cable. A switch whose fans spin but whose `SYST` LED never lights is not booting, and
there is nothing on the console to talk to.

Assign the switch a static management IP of `10.0.100.3`, mask `255.255.255.0`.

**Every field uses `10.0.100.0/24`, and it is not a per-site choice.** Driver station
software is hardcoded to find its FMS at `10.0.100.5`, and bioarena matches it:
`ServerIpAddress` in [network/switch.go](network/switch.go) is fixed at that address, the
driver station listener binds to it, and the switch's NTP server line points at it. A field
addressed anywhere else has a listener that never starts, logging
*"Change IP address to 10.0.100.5 and restart"*.

| Device | Address |
|--------|---------|
| Field controller Pi | 10.0.100.5 |
| Switch (management) | 10.0.100.3 |
| Access point        | 10.0.100.2 |

**So fields cannot share a network.** Two of them on one LAN or one VPN would both claim
`10.0.100.5`, and neither would work. Each field is a separate island: its own switch, its
own controller, no route between them. If you need to reach more than one from a single
machine, reach them one at a time — over a bench network, or by plugging into the field you
want.

That is a real constraint rather than a preference, and it is the reason every field is
wired and documented identically. Record each deployment in [docs/sites/](docs/sites) — the
addresses will be the same in every record, which is the point; what differs is the station
count, the hardware, and how the e-stops are wired.

The switch is named `bioSwitch` by the bootstrap script. Nothing reads the hostname —
bioarena reaches the switch by address over Telnet — so it exists only to tell you what you
are consoled into. Running more than one field is the case where that is worth overriding,
since the last two octets are identical at every field and the prompt is the quickest way to
tell them apart:

```bash
sudo python3 ~/switch-bootstrap.py --password <SWITCH_PASSWORD> --hostname bioSwitch2
```

Set an enable password when prompted — this is the password bioarena uses to authenticate over Telnet. Enter it in **Setup > Settings > Switch Password**.

VLAN assignments (fixed, managed automatically):

| Station | VLAN |
|---------|------|
| Red 1   | 10   |
| Red 2   | 20   |
| Red 3   | 30   |
| Blue 1  | 40   |
| Blue 2  | 50   |
| Blue 3  | 60   |

When a match loads, the controller pushes DHCP pool and IP configurations for each team's subnet over Telnet.

### Step 3 — Configure the field access point

The AP must run the Vivid-Hosting OpenWRT firmware with the REST API enabled. Bioarena communicates over HTTP. Set the AP address and password under **Arena → Settings**.

Specifically, bioarena needs plain HTTP on port 80 serving `POST /configuration` and
`GET /status`, the latter polled once a second and expected to report `ACTIVE`. That is
the **field firmware**, not the team-radio firmware — a radio in team mode serves no such
API and sits at `10.TE.AM.1` for whichever team it was last provisioned for.

**The AP lives at `10.0.100.2`**, on the management subnet alongside the Pi and the switch.
That is what goes in **AP Address** under Arena → Settings, and it needs nothing special:
the Pi's own `10.0.100.5` is in the same subnet, so they are neighbours.

**It also answers on `192.168.69.1`, as a backup.** That is where it turns up after a reset
or a failed firmware write, so the field deliberately carries that subnet too — otherwise an
AP that has dropped back is unreachable from the field entirely and recovering it means
cabling a laptop straight to it. Two things hold an address there, both applied for you:

- The Pi, via `ExecStartPre` in `bioarena.service` — `192.168.69.5/24` on `eth0`. No routing
  needed; the AP is on the same VLAN.
- The switch, via a secondary address on `Vlan1` — `192.168.69.2/24`, pushed with the rest
  of the baseline. This lets anything else on the field reach a fallen-back AP, and gives it
  a gateway if it ever wants one.

Without an address in that subnet a reset AP is simply unreachable: nothing has an interface
there, so there is nothing to ARP for and nothing to route through, and the badge sits at
`UNKNOWN` however healthy the AP is.

**The VH-113 takes passive 12 V from its own adapter, not 802.3af/at PoE.** It does not
negotiate, and its injector puts 12 V on the line unconditionally. On a field whose switch
is PoE-capable that is a hazard in both directions — the switch trying to source power into
a device that never asked for it, and the injector feeding 12 V back toward a port expecting
to be the supply. Power the AP from its own adapter and disable PoE on its port:

```
configure terminal
interface GigabitEthernet0/8
power inline never
end
```

Then `write memory`. Bioarena never emits a `power inline` command, so this is a manual step
that lives in the switch's saved configuration; re-check it after a `write erase`. A non-PoE
switch needs none of this.

**First-time VH-113 setup**

1. Laptop straight into the AP, static `192.168.69.100/24`, browse `http://192.168.69.1` —
   the backup address, which is where a factory-fresh unit answers.
2. Confirm or flash the field-mode image, following Vivid Hosting's own instructions.
3. Set the radio channel. It must match **AP Channel** under Arena → Settings, which is
   what gets pushed on every match load.
4. Put **AP Address** in Arena → Settings to `10.0.100.2`.

**The AP password is normally blank.** The practice firmware exposes no API token, and
that is a supported configuration rather than a workaround: bioarena adds the
`Authorization: Bearer` header only when the password field is non-empty, so a blank field
means unauthenticated calls, which is what this firmware expects. Confirm from the Pi:

```bash
curl -s http://10.0.100.2/status
```

JSON back means leave **AP Password** empty. A `401` means this build does want a token,
and on Vivid's field images that is usually the web UI's admin password. No answer at all
means the Pi cannot reach it — check `ip addr show eth0` for `10.0.100.5`, and try the
backup address `http://192.168.69.1` in case the AP has reset.

**Enter the address without a scheme.** `10.0.100.2`, not `http://10.0.100.2` — the
code prepends `http://`, so a typed prefix produces `http://http://10.0.100.2` and every
poll fails.

When a match loads, the controller pushes one SSID + WPA2 key per team (six total). The
SSID is the team number and the key is that team's WPA key from its record under
**Teams** — so each robot's VH-109 radio, provisioned for its team as it would be for a
competition, joins the correct VLAN with no field-side changes.

A team with no WPA key set is provisioned with an empty one. Set it on the team record
before the robot will associate.

### Step 4 — Verify Pi reachability

The Pi must be able to reach:

| Destination | Protocol | Port | How |
|---|---|---|---|
| Field AP, `10.0.100.2` | HTTP | 80 | Management subnet, same VLAN |
| Field AP backup, `192.168.69.1` | HTTP | 80 | Second address on `eth0`, same VLAN |
| Switch, `10.0.100.3` | Telnet | 23 | Same subnet as the FMS address |
| Team subnets, `10.TE.AM.0/24` | TCP 1750, UDP 1160 | | Routed via the switch |
| Staging subnets, `172.16.<vlan>.0/24` | TCP 1750 | | Routed via the switch |

The last two are the ones that catch people. The switch routes a driver station's packets
to the Pi without any help, so it looks like the path works — but the Pi's replies need a
route back, and the field deliberately has no default gateway. Without those routes a
driver station sits there connected to a field that never answers it, which looks like the
driver station's fault.

`bioarena.service` adds them:

```
ExecStartPre=-/sbin/ip route add 10.0.0.0/8 via 10.0.100.3 dev eth0
ExecStartPre=-/sbin/ip route add 172.16.0.0/12 via 10.0.100.3 dev eth0
```

Test from the Pi:

```bash
ping 10.0.100.2                   # the access point
curl -s http://10.0.100.2/status
ip route get 172.16.20.20         # a staging address: should route via 10.0.100.3
```

`Network is unreachable` from that last one means the routes are missing, and every driver
station will fail to connect no matter how healthy everything else looks.

### Step 5 — Serve time from the Pi

**Nothing on a practice field has a battery-backed clock.** The Pi restores the last known
time from `fake-hwclock` at boot; a Catalyst comes up believing it is 2004. With no route
to the internet neither corrects itself, so timestamps across the field disagree by years —
and the moment that costs you is the one where a match went wrong and you are trying to
line up the switch's log against bioarena's.

The field controller is the only sensible source, so it serves time to everything else.

**Nothing to do by hand.** Both halves are cooked into the scripts, for the same reason the
switch's VLANs are: a setup step that lives only in a document is a setup step that is
eventually skipped on a fresh Pi.

**On the Pi**, `deploy-fms.sh` installs chrony and lays down the drop-in. Raspberry Pi OS
ships `systemd-timesyncd`, which is a client only and cannot serve; there is no setting that
changes this. Debian's chrony package replaces timesyncd rather than running alongside it,
since two daemons steering one clock is worse than either alone.

The drop-in ([docs/chrony-bioarena.conf](docs/chrony-bioarena.conf)) carries
`local stratum 10`, which is the part that matters: chrony otherwise refuses to answer while
it is unsynchronised, which on an isolated field is always. Stratum 10 is deliberately poor,
so a real upstream source wins if the Pi ever reaches one.

`apt` needs internet, which a Pi already on the field does not have. **Deploy once with the
Pi online before wiring it into the field.** If the install cannot reach a mirror the deploy
warns rather than failing, puts the drop-in in place anyway, and finishes — re-run it once
the Pi has internet and that step completes.

**On the switch**, bioarena pushes `ntp server 10.0.100.5` as part of the baseline it applies
on its first configuration of a run, and saves it with `write memory` alongside everything
else. Confirm with `show ntp associations` after a few minutes — the switch polls slowly, so
it is not instant.

**This makes the field's clocks consistent, not correct.** Correct requires the Pi reaching
a real NTP server at some point, or being set by hand:

```bash
sudo timedatectl set-time "2026-08-08 14:30:00"
```

Worth doing before a session you might need to review afterwards. Consistent-but-wrong is
still enough to correlate two logs; disagreeing by years is not.

### Team subnet addressing

Each team's subnet is derived from the team number. Team 4834 uses `10.48.34.x`:

```
10. [first two digits] . [last two digits] . x
     48                   34
```

| Device         | Address          |
|----------------|------------------|
| Switch gateway | 10.TE.AM.4       |
| Robot (RoboRIO)| 10.TE.AM.2       |
| DS laptop      | 10.TE.AM.5 (DHCP)|

The DHCP pool reserves `.1`–`.19` and `.200`–`.254`. Addresses `.20`–`.199` are available for laptops and other devices.

## Usage

### Starting and stopping the service

```bash
sudo systemctl start bioarena
sudo systemctl stop bioarena
sudo systemctl restart bioarena
sudo systemctl status bioarena
```

### Viewing logs

Service output:

```bash
journalctl -u bioarena -f
```

Per-team driver-station packet logs are written to `logs/` on the Pi, one CSV per team
per match. Browse and download them from any device on the field network:

```
http://10.0.100.5:8080/logs/
```

The listing has no authentication, matching how `/static/` is served — anyone on the
field network can read them. They sit outside `static/` only so the deploy step does not
copy them to the Pi.

Or pull them off over SSH:

```bash
scp <USER>@<PI_ADDRESS>:/opt/bioarena/logs/\*.csv ./
```

The directory grows with every match. Clear it periodically:

```bash
ssh <USER>@10.0.100.5 "sudo rm -f /opt/bioarena/logs/*.csv"
```

### Running a practice match

Match Play does not record scores or results — it is a pure practice tool. Each match is a standalone timed run.

1. Open `http://10.0.100.5:8080` in a browser on any device on the field network.
2. Go to **Teams** and enter the team numbers for each station.
3. Go to **Match**.
4. Type team numbers into the station fields and click **Register** to assign them, then
   click **Bypass Empty** to bypass the stations with no team, or check **BYP** individually.
5. Wait for assigned stations to show a DS connection (or bypass them), then click **Start Match**.
6. The field returns to pre-match on its own a few seconds after the match ends, keeping
   the team assignments and bypasses, so another round can start immediately. **Clear
   Match** does the same thing straight away.

> **Do not run field control from a driver station laptop.** The driver station releases
> its IP address at the end of every match — see
> [Driver station behaviour worth knowing](#driver-station-behaviour-worth-knowing) — and
> any browser session that laptop had open to the field UI dies with the address, every
> round. The operator is thrown out of the field controls the instant the match ends. Use
> a separate device: a phone, a tablet, or the Pi itself.

Match timing defaults (2026 REBUILT):

| Period  | Duration |
|---------|----------|
| Auto    | 20 s     |
| Pause   | 3 s      |
| Teleop  | 140 s    |

### Driver station behaviour worth knowing

**The driver station releases its IP address when a match ends.** Its log shows
`Warning 44000 ipconfig /release`, followed by lost communication with the robot and a
brief FMS disconnect, and then it recovers on its own. This is the driver station's own
state machine, not the field: it happens on the match-end transition with no network change
behind it, it does *not* happen when Clear Match tears the network down, and it does *not*
happen when a robot is e-stopped at the end of a match — the same network treatment, a
different internal state.

It does not recover on its own. Windows releases the address and then waits for something
to change before asking for another — `ipconfig /renew` on the laptop brings it straight
back, and so does replugging the cable, because the link event is what prompts it.

So the field does that for you. Every thirty seconds, outside a match, a station that has a
team, a cable, and no driver station has its port cycled once, which produces the link event
and the laptop re-requests. A station with no team, no cable, or a working driver station is
left alone, and a sixty-second cooldown keeps a laptop with its driver station software
closed from being cycled repeatedly.

Expect up to half a minute of "No DS" after a match before it comes back by itself. It is
worth recognising because it looks exactly like a field fault, and chasing it through switch
logs finds nothing.

The one thing the field can do to make it worse is reconfigure while the driver station is
mid-renew. `preLoadNextMatch` runs five seconds after a match ends and rebuilds for the
next match's teams, cycling their station ports. With the same teams loaded the diff makes
that a no-op; with a changed lineup it lands in the recovery window, and those stations take
longer to come back.

### Running free practice

Free practice has no timers: registered robots are drivable continuously until the
operator stops them. Register each station first — the team's SSID and its VLAN subnet are
created on registration, so a driver station plugged in beforehand has nothing to get an
address from.

Three controls, and the difference between the last two matters:

| Control | Effect |
|---------|--------|
| **ENABLE FIELD** | Starts free practice, or resumes robots after a halt |
| **DISABLE FIELD** | Halts all robot operation. Teams stay registered, SSIDs stay up, team subnets stay configured, driver stations stay connected. **ENABLE FIELD** resumes immediately |
| **Reset Field** | Clears every slot, drops all SSIDs and team subnets, disconnects every driver station, and returns to setup |

Reach for **DISABLE FIELD** between runs. **Reset Field** is for ending the session or
starting over — after it, every station has to be registered again, and laptops re-request
an address, which takes up to one DHCP lease unless the port is unplugged and replugged.

Per-station E-stops remain functional throughout and are independent of both.

### Ports used by the service

| Port | Protocol | Purpose                          |
|------|----------|----------------------------------|
| 8080 | TCP/HTTP | Web UI and WebSocket updates     |
| 1750 | TCP      | Driver Station connection        |
| 1121 | UDP      | Enable/disable packets to DS     |
| 1160 | UDP      | Status packets from DS           |

## Configuration

Match timing and hardware drivers are configured in Settings inside the web UI. No config file is required for basic operation.

To change match timing, go to **Setup > Settings** and adjust the duration fields. Defaults:

| Setting                 | Default |
|-------------------------|---------|
| Auto duration           | 20 s    |
| Pause duration          | 3 s     |
| Teleop duration         | 140 s   |
| HTTP port               | 8080    |

Network credentials (AP address, AP password, switch address, switch password) are also set in the Settings page and stored in the local database.

## Field hardware

**Hub LEDs (DMX over Ethernet)**

The 2026 Hub lighting runs E1.31 sACN, ported from upstream cheesy-arena. It is
configured from **Arena → LEDs → DMX Hub LEDs** in the web UI and stored in the
database, so it survives a restart. A blank address disables output.

Practice fields with cheaper fixtures can override the layout — one fixture per alliance
Hub instead of eight — and select a fixture capability so per-pixel sequences degrade to
a solid colour. See
[docs/prd-half-field-match-simulation.md](docs/prd-half-field-match-simulation.md) for
the addressing rules.

**Field lights (serial)**

A separate, simpler interface for an Arduino-driven light or sound cue, independent of
the Hub LEDs:

```go
type FieldLights interface {
    SetState(state LightingState) error
}
```

Configured in `config.yaml`. Supported `field_lights_driver` values are `none` (the
default) and `serial`:

```yaml
field_lights_driver: "serial"
field_lights_port: "/dev/ttyUSB0"
field_lights_baud: 9600
field_lights_command: "START\n"
```

**E-stop panel**

> For full wiring diagrams, component list, and step-by-step assembly, see **[docs/hardware-wiring.md](docs/hardware-wiring.md)**.

Each alliance can have a dedicated Raspberry Pi wired to 7 inputs:

| Pin role           | Station              | Channels |
|--------------------|----------------------|----------|
| station1_estop     | R1 or B1 (e-stop)    | NC + NO  |
| station1_astop     | R1 or B1 (a-stop)    | NO       |
| station2_estop     | R2 or B2 (e-stop)    | NC + NO  |
| station2_astop     | R2 or B2 (a-stop)    | NO       |
| station3_estop     | R3 or B3 (e-stop)    | NC + NO  |
| station3_astop     | R3 or B3 (a-stop)    | NO       |
| field_estop        | all stations (e-stop) | NC + NO |

Wiring: e-stop buttons are **dual-channel**. Three conductors run to each button — common GND, the NC contact, and the NO contact — with the internal pull-up on both GPIO lines. Released, NC reads LOW and NO reads HIGH; pressed, they swap. Any other combination is a wiring fault, which stops the field and blocks match start until the wiring is repaired. A-stops stay single-channel NO.

Use latching mushroom-head buttons for e-stops and momentary pushbuttons for a-stops. The panel samples its pins locally at 100 Hz and reports the state of every configured input on each poll.

Recommended static IPs: `10.0.100.11` (red panel), `10.0.100.12` (blue panel).

Create `estop-panel.yaml` in the panel Pi's working directory:

```yaml
alliance: "red"       # "red" or "blue"
http_port: 8765
gpio_chip: "gpiochip0"
pins:
  # BCM GPIO numbers. {nc: 0, no: N} or a bare N is single-channel (no fault
  # detection); no: 0 means not wired and is skipped.
  station1_estop: {nc: 17, no: 4}
  station1_astop: 27
  station2_estop: {nc: 22, no: 12}
  station2_astop: 23
  station3_estop: {nc: 24, no: 13}
  station3_astop: 25
  field_estop: {nc: 5, no: 6}
```

Build and deploy:

```bash
./build-pi.sh          # produces estop-panel-pi alongside bioarena-pi
```

Deploy it the same way as the field controller, with the alliance — addresses again being
wherever each panel answers from your build machine, not its field address:

```bash
./deploy-panel.sh 192.168.1.43 red
```

```bash
./deploy-panel.sh 192.168.1.44 blue
```

The alliance is required because a panel Pi is not interchangeable: the script writes that
alliance's static address into the service file, creates the service account, and puts it
in the `gpio` group. That last one is what bites when it is done by hand — without it the
panel starts, logs that it cannot open the GPIO chip, and reports no stops, which is a
field that looks fine and has no working e-stops.

`deploy-fms.sh` does the same for the controller, which needs it whenever the **field**
e-stop is wired to the controller's own GPIO rather than to a panel. The symptom is
identical and just as quiet: `cannot open /dev/gpiochip0: permission denied` in the log,
after which the arena installs a panel that reports no stops at all — so the badge is green
and nothing is watching the button.

Group membership is taken at process start, so adding it by hand needs a restart, not a
settings save:

```bash
sudo usermod -aG gpio bioarena && sudo systemctl restart bioarena
```

Wire the main bioarena to the panel by adding to `config.yaml` and restarting:

```yaml
red_estop_panel:
  host: "http://10.0.100.11:8765"
blue_estop_panel:
  host: "http://10.0.100.12:8765"
```

Panel addresses can also be changed live via **Setup > Settings** without a restart.

The field runs normally without panel Pis; missing panels log a warning and return no stops.

### Hub LED output

Bioarena drives the hub lights over **E1.31 sACN** by default: unicast UDP to the address in
**Arena → Settings → LEDs**, port 5568, 24 consecutive DMX channels per fixture (8 pixels ×
3), with the universe and start channel set per alliance.

**Tick "Send Art-Net instead of sACN"** for a node that speaks only Art-Net. Same pixels,
same layout, same sequences — a different packet on UDP 6454. Most DMX gateways do both and
have a protocol setting of their own; make sure the two agree, because a node listening for
the wrong one simply stays dark.

**Everything stays on universe 1.** Both layouts put every fixture there — sixteen fixtures
at 24 channels each is 384 of the available 512, so a full field never needs a second — and
the universe number is sent unchanged in both protocols. So the gateway is set to universe 1
regardless of what it is speaking, and the instructions do not branch.

The Art-Net specification numbers universes from zero, so this is universe 1 of that scheme
rather than the first one. Gateways almost always label them from one in their own
interface, which is the number you are setting.

Split across universes only if a layout ever outgrows 512 channels: change the numbers in
Settings, and the controller sends one packet per universe.

## Development

Go 1.23+ — see `go.mod`.

**Repository layout**

| Path | Contents |
|------|----------|
| `main.go` | Entry point: loads `config.yaml`, builds the arena, starts the web server |
| `field/` | Arena state machine, driver station connections, match flow, free practice |
| `game/` | Scoring and match timing rules |
| `network/` | Access point, switch, and local team network drivers |
| `hardware/`, `plc/`, `led/` | Field hardware: e-stop panels, lights, sACN output |
| `web/`, `templates/`, `static/` | HTTP handlers, HTML templates, client-side assets |
| `model/` | BoltDB datastore and record types |
| `websocket/` | WebSocket plumbing for live UI updates |
| `cmd/estop-panel/` | Separate binary for the alliance e-stop panel Pis |
| `docs/` | Wiring, upstream divergences, serial console, site configurations |

The runtime database is BoltDB in `event.db`, created on first start and not tracked.

**Style**

Standard Go: tabs, `CamelCase` for exported names, `camelCase` otherwise, and short
domain-named packages matching the directories above. Run `gofmt` before submitting.

**First-time setup**

Node LTS is required for the JavaScript tests, which the pre-push hook runs:

```bash
winget install OpenJS.NodeJS.LTS
```

Then, in a new terminal so the updated `PATH` is picked up:

```bash
npm install
```

```bash
git config core.hooksPath .githooks
```

That last line enables [.githooks/pre-push](.githooks/pre-push), which runs both test
suites and refuses the push if either fails. Git stores `core.hooksPath` per clone, so
every clone needs it once.

**Run Go tests**

```bash
go test ./...
```

Tests are `*_test.go` files co-located with the package they cover. Target one with
`go test ./field -run TestName`. Prefer table-driven tests where a behaviour has several
cases.

**Run JavaScript tests**

Client-side behaviour (DOM manipulation, WebSocket message handlers, UI state) is tested with [Jest](https://jestjs.io/) using a jsdom environment.

```bash
npm install        # first time only
npm run test:js
```

Tests live in `static/js/__tests__/`. Each JS source file that contains non-trivial logic should have a corresponding `*.test.js` file. To make a file importable by Jest, add a `module.exports` guard at the bottom:

```javascript
if (typeof module !== "undefined") {
  module.exports = { myFunction };
}
```

Client-side behaviour cannot be caught by the Go tests, so any change to a `.js` file
needs Jest coverage of its state transitions — what a handler does when the server reports
an empty slot versus an occupied one, whether a user-typed value survives a status push,
and so on. Both suites must pass before committing; the pre-push hook enforces it.

**Run locally (no robots)**

```bash
go build -o bioarena
./bioarena
```

Open `http://localhost:8080`. No network hardware is required for testing.

**Build for Pi**

```bash
./build-pi.sh
```

Output: `bioarena-pi` and `estop-panel-pi` (ARM, statically linked, ready to copy to the
Pi). Add `ARCH=arm GOARM=7` for a 32-bit image.

### Raspberry Pi OS releases

Bioarena needs systemd and `ip(8)`, present on every current release, so the OS choice
comes down to what else changes.

| | Trixie / Bookworm | Buster and earlier |
|---|---|---|
| Build flag | `./build-pi.sh` (arm64) | `ARCH=arm GOARM=7 ./build-pi.sh` |
| Static IP | `nmcli` (NetworkManager) | `/etc/dhcpcd.conf` |
| apt | Works | End of life; repositories moved to `archive.debian.org` |

**Verify the GPIO chip name** if you use e-stop panels or GPIO field lights. Both default
to `gpiochip0`, which is correct for a Pi 4 today, but chip numbering has moved between
kernel versions and hardware generations. Confirm on the Pi with `gpiodetect`, and set
`gpio_chip` in `estop-panel.yaml` if it differs.

## Documentation

- [docs/hardware-wiring.md](docs/hardware-wiring.md) — field hardware wiring, opto-isolation, e-stop panel assembly.
- [docs/prd-half-field-match-simulation.md](docs/prd-half-field-match-simulation.md) — requirements for 1v0 half-field REBUILT 2026 simulation: AUTO outcome selection, FMS Game Data, DMX HUB light.
- [docs/upstream-divergences.md](docs/upstream-divergences.md) — where this fork differs from cheesy-arena, which differences are candidates to send upstream, and which files are kept byte-identical.
- [docs/console.py](docs/console.py) — serial console for the field switch, standard library only; Linux and macOS.
- [docs/chrony-bioarena.conf](docs/chrony-bioarena.conf) — chrony drop-in making the Pi the field's time source, so switch and controller logs can be correlated.
- [docs/switch-bootstrap.py](docs/switch-bootstrap.py) — brings a factory or reset Catalyst to the point bioarena can configure it over Telnet: address, passwords, boot image. Console cable, one command.
- [docs/sites/](docs/sites) — one record per deployed field: addresses, switch port map, and switch backup. [richmond.md](docs/sites/richmond.md) is the example to copy; [oakland.md](docs/sites/oakland.md) is a second field being stood up.

## Contributing

- Open a [GitHub issue](https://github.com/Team254/cheesy-arena/issues) for bugs or feature requests.
- Send a pull request with a clear summary.
- Include test notes: the exact commands run, for example `go test ./...` and
  `npm run test:js`.
- Include screenshots for any change to `web/`, `templates/`, or `static/`.

Commit messages use short imperative sentences, e.g. `Fix driver station TCP reads`, and
often carry an issue or PR number in parentheses: `Fix driver station TCP reads (#258)`.

## License

Teams may use this software freely for practice, scrimmages, and off-season events. See [LICENSE](LICENSE) for details.
