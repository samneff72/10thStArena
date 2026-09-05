#!/usr/bin/env bash
# Deploy bioarena to a field controller Pi.
#
#   ./deploy-fms.sh 192.168.1.42
#   ./deploy-fms.sh 192.168.1.42 admin      # if you log in as something other than admin
#
# The address is wherever the Pi answers FROM THIS MACHINE right now -- normally a bench or
# workshop network the two share. It is not the Pi's field address: bioarena.service gives
# it 10.0.100.5 on eth0 once it is wired into the field, which is how driver stations reach
# it, not how you reach it to deploy. Deploying over the bench network is the intended way
# round, and keeps this machine off the field network entirely.
#
# Finding it, easiest first:
#   ssh admin@raspberrypi.local     mDNS, using the hostname set when the card was flashed
#   hostname -I                     on the Pi, if it has a screen
#   or the bench router's client list
#
# Does everything, every time: builds, creates the service account if this Pi has never
# been deployed to, copies the binary and the web assets, installs the service, starts it,
# and checks that it came up. Nothing is optional and nothing is remembered between runs,
# because a deploy that needs you to know which flag to pass is a deploy that eventually
# goes out missing a file.
#
# Safe to run repeatedly. Safe to run on a Pi that has never seen bioarena.

set -euo pipefail

TARGET="${1:-}"
LOGIN="${2:-admin}"

if [ -z "$TARGET" ]; then
	echo "Usage: ./deploy-fms.sh <pi-address> [login-user]" >&2
	echo "" >&2
	echo "Example: ./deploy-fms.sh 192.168.1.42" >&2
	echo "" >&2
	echo "The address is the Pi's -- not the switch's or the access point's -- and the one" >&2
	echo "it answers on from this machine right now. That is normally a bench network you" >&2
	echo "share with it, not the field: the Pi's 10.0.100.5 is what it gives itself on" >&2
	echo "eth0 once wired in, for driver stations to reach, not for deploying over." >&2
	echo "" >&2
	echo "To find it: ssh admin@raspberrypi.local, or 'hostname -I' on the Pi, or the" >&2
	echo "bench router's client list." >&2
	exit 2
fi

REMOTE="$LOGIN@$TARGET"
STAGING=".bioarena-deploy"
step=0

announce() {
	step=$((step + 1))
	echo ""
	echo "[$step/7] $1"
}

fail() {
	echo "" >&2
	echo "DEPLOY FAILED: $1" >&2
	echo "" >&2
	echo "Fix the cause and run this again -- repeating it is safe." >&2
	exit 1
}

trap 'fail "step $step did not complete"' ERR

# One authenticated connection, shared by every ssh and scp below.
#
# This deploy opens about a dozen of them, and without multiplexing each is a separate
# authentication -- which is a dozen password prompts on a Pi without key auth. With a master
# connection the first one authenticates and the rest ride on it.
#
# No credential is stored anywhere to achieve this, and none should be: a password in this
# script would sit in the repository, in shell history, and in the process list. The lasting
# fix is key auth, which the closing hint points at.
#
# Guarded by platform, because support cannot be probed usefully. On Git Bash and other
# MSYS/Cygwin environments ssh accepts every multiplexing option and then fails at connect
# time with "Failed to connect to new control master": the control socket is a Unix domain
# socket, and the emulation layer does not carry the mux protocol over it. "ssh -G" only
# resolves options and never connects, so it reports success on exactly the systems where
# this breaks -- which is how it shipped broken once already.
#
# Set BIOARENA_SSH_MUX=0 to force it off, or =1 to force it on if your environment does
# support it and the check below is being too cautious.
SSH_OPTS=()
CONTROL_DIR=""
mux_wanted="${BIOARENA_SSH_MUX:-auto}"
if [ "$mux_wanted" = "auto" ]; then
	case "$(uname -s 2>/dev/null || echo unknown)" in
	MINGW* | MSYS* | CYGWIN* | unknown) mux_wanted=0 ;;
	*) mux_wanted=1 ;;
	esac
fi
if [ "$mux_wanted" = "1" ] && command -v mktemp >/dev/null 2>&1; then
	CONTROL_DIR="$(mktemp -d 2>/dev/null || true)"
	if [ -n "$CONTROL_DIR" ]; then
		# %C is a hash of the connection rather than "user@host:port" -- no colon, which is
		# not a legal path character everywhere this might run.
		SSH_OPTS=(
			-o ControlMaster=auto
			-o "ControlPath=$CONTROL_DIR/cm-%C"
			-o ControlPersist=120
		)
	fi
fi

cleanup_control() {
	# Preserved and re-raised at the end: this runs on EXIT, and a function that finishes
	# non-zero would otherwise rewrite the script's exit status into a success.
	local status=$?
	if [ ${#SSH_OPTS[@]} -gt 0 ]; then
		# Closes the shared connection rather than leaving it open for ControlPersist to
		# time out, so a finished deploy holds nothing on the Pi.
		ssh "${SSH_OPTS[@]}" -O exit "$REMOTE" >/dev/null 2>&1 || true
	fi
	if [ -n "$CONTROL_DIR" ]; then
		rm -rf "$CONTROL_DIR"
	fi
	return $status
}
trap cleanup_control EXIT

announce "Building for the Pi"
GOOS=linux GOARCH=arm64 go build -o bioarena-pi .
echo "      bioarena-pi built"

announce "Copying to $TARGET"
# Into the login user's home first, which needs no privileges. Everything that does is
# done in one go below, so the Pi asks for a sudo password once rather than at every step.
ssh "${SSH_OPTS[@]}" -o ConnectTimeout=10 "$REMOTE" "rm -rf ~/$STAGING && mkdir -p ~/$STAGING"
scp "${SSH_OPTS[@]}" -q bioarena-pi bioarena.service "$REMOTE:~/$STAGING/"
scp "${SSH_OPTS[@]}" -qr static templates "$REMOTE:~/$STAGING/"
echo "      binary, service file, static, templates"

announce "Installing (the Pi may ask for your password)"
# -t so sudo has a terminal to prompt on: without it sudo refuses with "a terminal is
# required to read the password", which reads like a bug in the deploy rather than a
# missing tty.
ssh "${SSH_OPTS[@]}" -t "$REMOTE" "
	set -e
	# Idempotent: creates the service account the first time, does nothing after. It is a
	# system user with no login, so a field controller does not depend on which username
	# the SD card was flashed with.
	id bioarena >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/bioarena --shell /usr/sbin/nologin bioarena
	sudo mkdir -p /opt/bioarena
	sudo chown bioarena:bioarena /opt/bioarena

	# The directories the service writes into, owned by the service. It creates them
	# itself on a clean install, but only if it can write to /opt/bioarena -- and a
	# directory left owned by someone else makes match logging fail mid-match with a
	# permission error, which is a poor time to find out.
	sudo install -d -o bioarena -g bioarena /opt/bioarena/logs /opt/bioarena/db
	[ -f /opt/bioarena/event.db ] && sudo chown bioarena:bioarena /opt/bioarena/event.db || true

	# Stopped before the binary is replaced: Linux refuses to overwrite a running
	# executable, and the error reads like a permissions problem.
	sudo systemctl stop bioarena 2>/dev/null || true

	sudo install -o bioarena -g bioarena -m 755 ~/$STAGING/bioarena-pi /opt/bioarena/bioarena-pi
	sudo cp -r ~/$STAGING/static ~/$STAGING/templates /opt/bioarena/
	sudo chown -R bioarena:bioarena /opt/bioarena/static /opt/bioarena/templates
	sudo cp ~/$STAGING/bioarena.service /etc/systemd/system/bioarena.service
	sudo systemctl daemon-reload
	sudo systemctl enable bioarena >/dev/null 2>&1
	sudo systemctl start bioarena
	rm -rf ~/$STAGING
"
echo "      installed to /opt/bioarena"

announce "Copying the switch tools"
# The console tools live on the Pi because that is where they run: both drive a USB serial
# cable to the switch's console port, and the Pi is the machine on the field with one.
#
# Copied on every deploy rather than by hand, because the moment you need them is the moment
# you cannot easily get files onto the Pi -- commissioning a switch that has no address yet,
# or rescuing a field that will not come up. A tool that has to be fetched during an outage
# is a tool that is not there.
#
# Copied only, never run. switch-bootstrap.py needs a console cable that is not plugged into
# a working field, and a switch password this script has no business knowing; running it
# from here would fail on healthy hardware and teach you to ignore deploy warnings. It is
# also a once-per-switch step, where this is a once-per-change one.
scp "${SSH_OPTS[@]}" -q docs/switch-bootstrap.py docs/console.py "$REMOTE:~/"
echo "      ~/switch-bootstrap.py and ~/console.py (run these on the Pi, cable in hand)"

announce "Serving time from the Pi"
# The field controller is the field's clock. Nothing here has a battery-backed clock, and
# with no route to the internet nothing corrects itself, so log timestamps across the field
# drift years apart -- which is discovered at the worst moment, lining up the switch's log
# against bioarena's after a bad match.
#
# Raspberry Pi OS ships systemd-timesyncd, which is a client only and cannot serve; no
# setting changes that. Debian's chrony package replaces it rather than running alongside,
# since two daemons steering one clock is worse than either alone.
#
# apt needs internet, which a Pi already on the field does not have. So the install is
# attempted and its failure is a warning rather than a failed deploy: the rest of this
# step still lands, and re-running the deploy once the Pi has internet finishes the job.
scp "${SSH_OPTS[@]}" -q docs/chrony-bioarena.conf "$REMOTE:~/chrony-bioarena.conf"
ssh "${SSH_OPTS[@]}" -t "$REMOTE" "
	set -e
	if ! command -v chronyd >/dev/null 2>&1; then
		echo '      chrony not installed; fetching it (this needs internet)'
		if sudo DEBIAN_FRONTEND=noninteractive apt-get install -y chrony; then
			echo '      chrony installed'
		else
			echo '      WARNING: could not install chrony -- no internet?' >&2
			echo '      The drop-in is in place. Re-run this deploy with the Pi online.' >&2
		fi
	fi

	# Copied whether or not chrony is installed yet, so it is already correct when it is.
	if [ -d /etc/chrony/conf.d ]; then
		sudo install -m 644 ~/chrony-bioarena.conf /etc/chrony/conf.d/bioarena.conf
		sudo systemctl restart chrony 2>/dev/null || true
	fi
	rm -f ~/chrony-bioarena.conf
"
echo "      the field takes its time from this Pi"

announce "Installing the kiosk browser autostart"
# Into the login user's home, because it runs in their desktop session rather than as the
# service. Harmless on a headless Pi: the autostart entry is simply never read.
scp "${SSH_OPTS[@]}" -q docs/kiosk/bioarena-kiosk.sh "$REMOTE:~/bioarena-kiosk.sh"
# Single-quoted so $HOME is the Pi's, not this machine's.
ssh "${SSH_OPTS[@]}" "$REMOTE" '
	set -e
	mkdir -p ~/.local/bin ~/.config/autostart
	install -m 755 ~/bioarena-kiosk.sh ~/.local/bin/bioarena-kiosk.sh
	rm -f ~/bioarena-kiosk.sh
	KIOSK="$HOME/.local/bin/bioarena-kiosk.sh"

	# Every autostart mechanism the Pi desktop might be using, because they disagree and
	# the wrong one fails by simply never running: an XDG entry under a compositor that
	# ignores XDG looks exactly like a broken script.
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
'
echo "      the display will open http://localhost:8080 at login"

announce "Checking it stayed up"
sleep 2
if ! ssh "${SSH_OPTS[@]}" "$REMOTE" "systemctl is-active --quiet bioarena"; then
	echo "" >&2
	echo "DEPLOY FAILED: the service started and then stopped." >&2
	echo "" >&2
	ssh "${SSH_OPTS[@]}" "$REMOTE" "journalctl -u bioarena -n 20 --no-pager" >&2 || true
	exit 1
fi

trap - ERR
echo "      running"
echo ""
echo "Done. Open the field at http://$TARGET:8080"
echo ""
echo "If something looks wrong, watch what it is doing:"
echo "  ssh $REMOTE 'journalctl -u bioarena -f'"
echo ""
echo "Tired of typing your password? Copy a key up once and this stops asking:"
echo "  ssh-keygen -t ed25519        # only if you do not already have one"
echo "  ssh-copy-id $REMOTE"
echo ""
echo "Git Bash on Windows has no ssh-copy-id; do the same thing by hand:"
echo "  cat ~/.ssh/id_ed25519.pub | ssh $REMOTE \"mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys\""
echo ""
echo "That covers the login. The Pi may still ask for sudo during the install step; if that"
echo "is worth removing too, it is a sudoers rule you add on the Pi yourself -- this script"
echo "deliberately carries no credentials."
echo ""
echo "Commissioning a switch, or one that will not answer? The tools are on the Pi. Put a"
echo "USB console cable between it and the switch's console port, then:"
echo "  ssh $REMOTE"
echo "  sudo python3 ~/switch-bootstrap.py --password <PASSWORD>   # address, passwords, boot image"
echo "  python3 ~/console.py                                       # only if that did not do it"
echo ""
echo "Bootstrap first. It is idempotent, so it costs nothing on a switch already done, and"
echo "it tells you what it found -- which is usually the answer you wanted from the console."
echo "Reach for console.py when it reports rejected commands or cannot find an IOS image; a"
echo "'switch:' prompt there means the switch never loaded IOS and is forwarding nothing."
echo ""
echo "Do not leave console.py running when you bootstrap: it holds the serial port open, and"
echo "the bootstrap cannot open the device while it does. Ctrl-] to exit it first."
