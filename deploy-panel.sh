#!/usr/bin/env bash
# Deploy the e-stop panel service to an alliance panel Pi.
#
#   ./deploy-panel.sh 192.168.1.43 red
#   ./deploy-panel.sh 192.168.1.44 blue
#   ./deploy-panel.sh 192.168.1.43 red admin   # if you log in as something other than admin
#
# Two different addresses are in play here, and confusing them is the easy mistake. The
# argument is where the panel Pi answers FROM THIS MACHINE right now -- a bench network the
# two share, and nothing to do with the field. The alliance is what decides the static field
# address written into its service file, 10.0.100.11 for red and .12 for blue.
#
# The alliance is required because a panel Pi is not interchangeable: it takes the static
# address the field controller expects to poll for that alliance, and the wrong one gives
# two panels the same address and a field whose e-stops answer for the wrong side.
#
# Finding a panel on the bench network, easiest first:
#   ssh admin@raspberrypi.local     mDNS, using the hostname set when the card was flashed
#   hostname -I                     on the Pi, if it has a screen
#   or the bench router's client list
#
# Flashing both panels with distinct hostnames is worth the minute it costs -- two Pis both
# answering to raspberrypi.local is a coin flip over which one you just deployed to.
#
# Does everything, every time: builds, creates the service account if this Pi has never
# been deployed to, puts it in the gpio group, writes the alliance's address into the
# service file, installs, starts, and checks it came up.
#
# Safe to run repeatedly. Safe to run on a Pi that has never seen the panel service.

set -euo pipefail

TARGET="${1:-}"
ALLIANCE="$(echo "${2:-}" | tr '[:upper:]' '[:lower:]')"
LOGIN="${3:-admin}"

usage() {
	echo "Usage: ./deploy-panel.sh <panel-pi-address> <red|blue> [login-user]" >&2
	echo "" >&2
	echo "Examples: ./deploy-panel.sh 192.168.1.43 red" >&2
	echo "          ./deploy-panel.sh 192.168.1.44 blue" >&2
	echo "" >&2
	echo "The address is where the panel answers from this machine right now -- normally a" >&2
	echo "bench network you share with it. Find it with ssh admin@raspberrypi.local, or" >&2
	echo "'hostname -I' on the Pi, or the bench router's client list." >&2
	echo "" >&2
	echo "The alliance is separate: it writes the static FIELD address into the service" >&2
	echo "file -- red 10.0.100.11, blue 10.0.100.12 -- which is what the field controller" >&2
	echo "polls, set under Arena > Settings." >&2
	exit 2
}

[ -n "$TARGET" ] || usage
case "$ALLIANCE" in
red) PANEL_ADDRESS="10.0.100.11" ;;
blue) PANEL_ADDRESS="10.0.100.12" ;;
*) usage ;;
esac

REMOTE="$LOGIN@$TARGET"
STAGING=".bioarena-deploy"
SERVICE_FILE="$(mktemp)"
trap 'rm -f "$SERVICE_FILE"' EXIT
step=0

# Where the panel's kiosk display points. Not a setting: driver station software is
# hardcoded to find its FMS at this address, so the field controller is always here.
FMS_ADDRESS="10.0.100.5"

announce() {
	step=$((step + 1))
	echo ""
	echo "[$step/5] $1"
}

fail() {
	echo "" >&2
	echo "DEPLOY FAILED: $1" >&2
	echo "" >&2
	echo "Fix the cause and run this again -- repeating it is safe." >&2
	exit 1
}

trap 'fail "step $step did not complete"' ERR

announce "Building for the $ALLIANCE panel"
GOOS=linux GOARCH=arm64 go build -o estop-panel-pi ./cmd/estop-panel
# The address lives in the service file, which ships with the red one. Editing it by hand
# per panel is the step people forget, and forgetting it puts two panels on one address.
sed "s#ip addr add 10\.0\.100\.[0-9]\+/24#ip addr add $PANEL_ADDRESS/24#" \
	cmd/estop-panel/estop-panel.service >"$SERVICE_FILE"
grep -q "$PANEL_ADDRESS/24" "$SERVICE_FILE" || fail "could not set the address in estop-panel.service"
echo "      estop-panel-pi built, service file set to $PANEL_ADDRESS"

announce "Copying to $TARGET"
# Into the login user's home first, which needs no privileges. Everything that does is done
# in one go below, so the Pi asks for a sudo password once rather than at every step.
ssh -o ConnectTimeout=10 "$REMOTE" "rm -rf ~/$STAGING && mkdir -p ~/$STAGING"
scp -q estop-panel-pi "$REMOTE:~/$STAGING/estop-panel"
scp -q "$SERVICE_FILE" "$REMOTE:~/$STAGING/estop-panel.service"
if [ -f estop-panel.yaml ]; then
	scp -q estop-panel.yaml "$REMOTE:~/$STAGING/"
fi
echo "      binary and service file"

announce "Installing (the Pi may ask for your password)"
# -t so sudo has a terminal to prompt on: without it sudo refuses with "a terminal is
# required to read the password", which reads like a bug in the deploy rather than a
# missing tty.
ssh -t "$REMOTE" "
	set -e
	id bioarena >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/estop-panel --shell /usr/sbin/nologin bioarena
	# The gpio group is the one that bites: without it the panel starts, reports that it
	# cannot open the GPIO chip, and then reports no stops at all -- a field that looks
	# healthy with e-stops that do nothing.
	sudo usermod -aG gpio bioarena
	sudo mkdir -p /opt/estop-panel
	sudo chown bioarena:bioarena /opt/estop-panel

	sudo systemctl stop estop-panel 2>/dev/null || true

	sudo install -o bioarena -g bioarena -m 755 ~/$STAGING/estop-panel /opt/estop-panel/estop-panel
	if [ -f ~/$STAGING/estop-panel.yaml ]; then
		sudo install -o bioarena -g bioarena -m 644 ~/$STAGING/estop-panel.yaml /opt/estop-panel/estop-panel.yaml
	fi
	sudo cp ~/$STAGING/estop-panel.service /etc/systemd/system/estop-panel.service
	sudo systemctl daemon-reload
	sudo systemctl enable estop-panel >/dev/null 2>&1
	sudo systemctl start estop-panel
	rm -rf ~/$STAGING
"
echo "      installed to /opt/estop-panel"

announce "Installing the kiosk browser autostart"
# The same kiosk as the field controller, pointed at the controller rather than at this Pi.
# A panel runs estop-panel on 8765 and has no web UI of its own, so localhost would never
# answer and the script would wait forever.
#
# 10.0.100.5 is not a choice: the driver station software is hardcoded to find its FMS
# there, so the controller is always at that address on a field.
#
# Into the login user's home, because it runs in their desktop session rather than as the
# service. Harmless on a headless panel: the autostart entry is simply never read, which is
# why this runs unconditionally rather than trying to detect a display.
scp -q docs/kiosk/bioarena-kiosk.sh "$REMOTE:~/bioarena-kiosk.sh"
# Single-quoted so $HOME is the Pi's, not this machine's; the URL is passed in separately.
ssh "$REMOTE" "FMS_URL=http://$FMS_ADDRESS:8080 "'sh -s' <<'REMOTE_KIOSK'
set -e
mkdir -p ~/.local/bin ~/.config/autostart
install -m 755 ~/bioarena-kiosk.sh ~/.local/bin/bioarena-kiosk.sh
rm -f ~/bioarena-kiosk.sh
KIOSK="$HOME/.local/bin/bioarena-kiosk.sh $FMS_URL"

# Every autostart mechanism the Pi desktop might be using, because they disagree and the
# wrong one fails by simply never running.
{
	echo "[Desktop Entry]"
	echo "Type=Application"
	echo "Name=Bioarena field"
	echo "Comment=Open the field controller full screen at startup"
	echo "Exec=$KIOSK"
	echo "X-GNOME-Autostart-enabled=true"
} > ~/.config/autostart/bioarena-kiosk.desktop

# labwc, the Trixie default, runs its own shell script and ignores XDG entries.
if command -v labwc >/dev/null 2>&1; then
	mkdir -p ~/.config/labwc
	touch ~/.config/labwc/autostart
	grep -q bioarena-kiosk ~/.config/labwc/autostart || echo "$KIOSK &" >> ~/.config/labwc/autostart
	chmod +x ~/.config/labwc/autostart
fi

# wayfire, the Bookworm default on a Pi 4, takes an [autostart] section in its ini.
if command -v wayfire >/dev/null 2>&1; then
	touch ~/.config/wayfire.ini
	grep -q "^\[autostart\]" ~/.config/wayfire.ini || echo "[autostart]" >> ~/.config/wayfire.ini
	grep -q bioarena-kiosk ~/.config/wayfire.ini || sed -i "/^\[autostart\]/a bioarena = $KIOSK" ~/.config/wayfire.ini
fi
REMOTE_KIOSK
echo "      the display will open http://$FMS_ADDRESS:8080 at login"

announce "Checking it stayed up"
sleep 2
if ! ssh "$REMOTE" "systemctl is-active --quiet estop-panel"; then
	echo "" >&2
	echo "DEPLOY FAILED: the panel service started and then stopped." >&2
	echo "" >&2
	ssh "$REMOTE" "journalctl -u estop-panel -n 20 --no-pager" >&2 || true
	exit 1
fi

trap - ERR
echo "      running at $PANEL_ADDRESS"
echo ""
echo "Done. Tell the field controller about it under Arena > Settings:"
if [ "$ALLIANCE" = "red" ]; then
	echo "  Red E-Stop Panel Address: http://$PANEL_ADDRESS:8765"
else
	echo "  Blue E-Stop Panel Address: http://$PANEL_ADDRESS:8765"
fi
echo ""
echo "Then press the button and watch the field react. If it does not:"
echo "  ssh $REMOTE 'journalctl -u estop-panel -f'"
