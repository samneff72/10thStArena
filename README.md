# Practice Field Controller

A Raspberry Pi service for running FRC practice sessions. Controls up to 6 robots across red and blue alliances. Runs timed auto and teleop periods. Manages the field access point and VLAN isolation automatically. Accessible from any browser on the field network.

## Requirements

- Raspberry Pi 4 (armv7 / 32-bit Raspberry Pi OS recommended)
- [Go 1.23+](https://golang.org/dl/) on your build machine
- Vivid-Hosting VH-113 field access point (running OpenWRT)
- Cisco Catalyst 3500-series managed switch
- Static IP assigned to Pi (recommend `10.0.100.5`)

## Install

**Build the Pi binary**

Run this on your development machine (not on the Pi):

```bash
./build-pi.sh
```

This cross-compiles an ARM binary named `bioarena`.

**Copy files to the Pi**

```bash
scp bioarena pi@<PI_IP>:~/bioarena/
scp -r static templates font schedules audio pi@<PI_IP>:~/bioarena/
scp bioarena.service pi@<PI_IP>:~/
```

**Install the systemd service (run on the Pi)**

```bash
sudo mv ~/bioarena.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable bioarena
sudo systemctl start bioarena
```

The service automatically assigns `10.0.100.5/24` to `eth0` on startup.

**Open the web UI**

```
http://10.0.100.5:8080
```

## Network Setup

This section is the most important part of the physical field setup. Read it carefully before powering anything on.

### Why this network layout matters

FRC Driver Station software is hardcoded to contact its FMS at `10.0.100.5` on ports `1750` (TCP) and `1121`/`1160` (UDP). The Pi must live at that address on the wired field network. Each robot lives on its own team-number-derived subnet isolated by a VLAN. The access point handles wireless; the switch enforces isolation.

### Topology

```
                        ┌─────────────────────────────────────┐
                        │   Cisco 3500 Managed Switch          │
                        │                                       │
          ┌─────────────┤ Trunk port        6 x access ports   │
          │             │                   (one per station)   │
          │             └──────────────────────┬────────────────┘
          │                                    │ (wired robot connections)
    ┌─────┴──────┐                      ┌──────┴──────┐
    │ Raspberry  │                      │  Robots      │
    │ Pi 4       │                      │  (RoboRIO)   │
    │ 10.0.100.5 │                      │  10.TE.AM.xx │
    └─────┬──────┘                      └─────────────┘
          │
          │ (HTTP to AP, Telnet to switch)
          │
    ┌─────┴────────────┐
    │ Vivid-Hosting    │
    │ VH-113 AP        │
    │ (OpenWRT)        │
    └─────┬────────────┘
          │ (WiFi — one SSID per team)
          │
    ┌─────┴──────────────┐
    │  DS Laptops        │
    │  (one per station) │
    │  10.TE.AM.5        │
    └────────────────────┘
```

### Step 1 — Assign a static IP to the Pi

The Pi must have `10.0.100.5` on the interface connected to the switch. The systemd service handles this automatically via:

```
ExecStartPre=/sbin/ip addr add 10.0.100.5/24 dev eth0
```

If you need a permanent static IP (survives reboots without the service), edit `/etc/dhcpcd.conf` on the Pi:

```
interface eth0
static ip_address=10.0.100.5/24
```

Do not put the Pi on a robot subnet (`10.TE.AM.x`). Use a dedicated management subnet such as `10.0.100.0/24`.

### Step 2 — Configure the Cisco 3500 switch

The switch must support VLANs. Bioarena configures it automatically over Telnet (port 23). You must:

1. Enable Telnet access with a password.
2. Create VLANs 10, 20, 30, 40, 50, 60 (one per alliance station).
3. Set the Pi's port as a trunk carrying all VLANs.
4. Set each robot's port as an access port in the correct VLAN.

The switch address and password are set in the Bioarena web UI under Settings > Network.

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

The AP must run the Vivid-Hosting OpenWRT firmware with the REST API enabled. Bioarena communicates over HTTP. Set the AP address and password in Settings > Network.

When a match loads, the controller pushes one SSID + WPA2 key per team (six total). Driver Station laptops connect to their team's SSID and land on the correct VLAN.

### Step 4 — Verify Pi reachability

The Pi must be able to reach:

| Destination          | Protocol | Port |
|----------------------|----------|------|
| Field AP             | HTTP     | 80   |
| Cisco switch         | Telnet   | 23   |
| Each robot subnet    | UDP      | 1160 |

Test from the Pi:

```bash
ping 10.0.100.5        # self
curl http://<AP_IP>/status
telnet <SWITCH_IP> 23
```

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

```bash
journalctl -u bioarena -f
```

### Running a practice match

Match Play does not record scores or results — it is a pure practice tool. Each match is a standalone timed run.

1. Open `http://10.0.100.5:8080` in a browser on any device on the field network.
2. Go to **Setup > Teams** and enter the team numbers for each station.
3. Go to **Match Play**.
4. Type team numbers into the station fields and click **Register** to assign them, or check **BYP** to bypass empty stations.
5. Wait for assigned stations to show a DS connection (or bypass them), then click **Start Match**.
6. After the match ends, click **Clear Match** to reset and run another round.

Match timing defaults (2026 REBUILT):

| Period  | Duration |
|---------|----------|
| Auto    | 20 s     |
| Pause   | 3 s      |
| Teleop  | 140 s    |

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

## Extending

Two hardware interfaces are reserved for future physical field hardware.

**Field lights**

Implement the `FieldLights` interface to drive field LEDs (e.g., via GPIO):

```go
type FieldLights interface {
    SetColor(r, g, b uint8)
    Off()
}
```

Set `field_lights_driver: "gpio"` in your build configuration to activate.

**E-stop panel**

Each alliance can have a dedicated Raspberry Pi wired to 7 GPIO inputs:

| Pin role           | Station              |
|--------------------|----------------------|
| station1_estop     | R1 or B1 (e-stop)    |
| station1_astop     | R1 or B1 (a-stop)    |
| station2_estop     | R2 or B2 (e-stop)    |
| station2_astop     | R2 or B2 (a-stop)    |
| station3_estop     | R3 or B3 (e-stop)    |
| station3_astop     | R3 or B3 (a-stop)    |
| field_estop        | all stations (e-stop) |

Wiring: NC (normally-closed) contacts between each GPIO pin and GND. Internal pull-up enabled; pin reads LOW (0) when the button is pressed (active-low). Buttons self-latch in hardware; the panel reports current pin state on every poll.

Recommended static IPs: `10.0.100.11` (red panel), `10.0.100.12` (blue panel).

Create `estop-panel.yaml` in the panel Pi's working directory:

```yaml
alliance: "red"       # "red" or "blue"
http_port: 8765
gpio_chip: "gpiochip0"
pins:
  station1_estop: 17  # BCM GPIO; 0 = not wired, skipped
  station1_astop: 27
  station2_estop: 22
  station2_astop: 23
  station3_estop: 24
  station3_astop: 25
  field_estop: 5
```

Build and deploy:

```bash
./build-pi.sh          # produces estop-panel binary alongside bioarena
scp estop-panel pi@10.0.100.11:~/estop-panel/
scp estop-panel.yaml   pi@10.0.100.11:~/estop-panel/
# Edit ExecStartPre IP in estop-panel.service, then:
scp cmd/estop-panel/estop-panel.service pi@10.0.100.11:~/
# On the panel Pi:
sudo mv ~/estop-panel.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now estop-panel
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

## Development

**Run Go tests**

```bash
go test ./...
```

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

Output: `bioarena` (ARM, statically linked, ready to copy to the Pi).

## Contributing

- Open a [GitHub issue](https://github.com/Team254/cheesy-arena/issues) for bugs or feature requests.
- Send a pull request with a clear summary and `go test ./...` results.
- Include screenshots for any UI changes.

Commit messages use short imperative sentences, e.g. `Fix driver station TCP reads`.

## License

Teams may use this software freely for practice, scrimmages, and off-season events. See [LICENSE](LICENSE) for details.
