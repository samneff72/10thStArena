#!/bin/sh
# Open the field controller full screen on this Pi's display, at startup.
#
# Installed into ~/.local/bin and launched by the autostart entry alongside it. Runs in the
# desktop session as the login user, not as the service.
#
# The address is a parameter because two kinds of Pi run this. On the field controller the
# server is local, which is the default. On an e-stop panel Pi there is no bioarena to talk
# to -- it runs estop-panel on 8765 -- so deploy-panel.sh points this at the controller
# instead, and the panel shows the same operating page as every other screen.
#
#   bioarena-kiosk.sh                          the local server
#   bioarena-kiosk.sh http://10.0.100.5:8080   a controller elsewhere on the field

URL="${1:-${BIOARENA_KIOSK_URL:-http://localhost:8080}}"

# The desktop session is usually up before bioarena is: the service waits on the network,
# and the switch configuration it does at startup takes a few seconds more. Opening the
# browser first would land on a connection error and stay there, so wait for an answer
# rather than racing it.
#
# On a panel Pi the wait is longer and less predictable, since it is waiting on another
# machine that may not be powered yet. Waiting indefinitely is still the right behaviour:
# a panel that comes up first should show the field when the field appears, not an error
# page that someone has to notice and reload.
while ! curl -sf -o /dev/null "$URL"; do
	sleep 2
done

# chromium-browser on Bookworm, chromium on Trixie.
BROWSER="$(command -v chromium-browser || command -v chromium)"
if [ -z "$BROWSER" ]; then
	echo "No chromium found. Install it with: sudo apt install chromium" >&2
	exit 1
fi

# --kiosk is the point. The rest suppress the things a browser does on a machine that gets
# powered off at the wall: the restore-pages bubble, update nagging, and the infobar that
# eats screen space on a field display nobody is going to click.
exec "$BROWSER" \
	--kiosk \
	--noerrdialogs \
	--disable-infobars \
	--disable-session-crashed-bubble \
	--check-for-update-interval=31536000 \
	"$URL"
