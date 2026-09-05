#!/usr/bin/env bash
# Cross-compile bioarena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets 64-bit Raspberry Pi OS (aarch64), which is what Trixie and Bookworm
# install by default.  Confirm with "uname -m" on the Pi:
#
#   aarch64   ARCH=arm64                     (the default below)
#   armv7l    ARCH=arm  with GOARM=7 set     (32-bit images, including Buster)
#
# Override without editing this file:
#   ARCH=arm GOARM=7 ./build-pi.sh
#
# A binary built for the wrong one does not fail usefully -- the kernel refuses
# to run it and reports "cannot execute binary file: Exec format error".
#
# Output: bioarena-pi  estop-panel-pi  (Linux/ARM binaries, copy to the Pi)
# Note: outputs use a "-pi" suffix so they never shadow a local Windows build.

set -euo pipefail

OUTPUT="bioarena-pi"
PANEL_OUTPUT="estop-panel-pi"

ARCH="${ARCH:-arm64}"
GOARM="${GOARM:-}"
export GOOS=linux GOARCH="$ARCH"
if [ "$ARCH" = "arm" ]; then
	export GOARM="${GOARM:-7}"
	DESCRIPTION="linux/arm (armv7 / 32-bit Raspberry Pi OS)"
else
	DESCRIPTION="linux/arm64 (aarch64 / 64-bit Raspberry Pi OS)"
fi

echo "Building bioarena for $DESCRIPTION..."
go build -o "$OUTPUT" .

echo "Building estop-panel for $DESCRIPTION..."
go build -o "$PANEL_OUTPUT" ./cmd/estop-panel

echo "Done: $OUTPUT  $PANEL_OUTPUT"
echo ""
echo "Deploy with the scripts -- they create the service account on a Pi that has never"
echo "been deployed to, copy everything, install the service, and check it stayed up:"
echo "       ./deploy-fms.sh 192.168.1.42            # field controller"
echo "       ./deploy-panel.sh 192.168.1.43 red      # e-stop panel, per alliance"
echo "       ./deploy-panel.sh 192.168.1.44 blue"
echo ""
echo "Those addresses are examples. Pass the address the Pi is reachable at FROM THIS"
echo "MACHINE right now -- typically a bench or workshop network you share with it, not"
echo "the field. The field addresses (10.0.100.x below) are what each Pi gives itself once"
echo "wired in, and are not how you reach it to deploy. A panel still takes its alliance"
echo "as an argument: that is what decides the static field address written into its"
echo "service file, independent of where you deployed from."
echo ""
echo "To find a Pi on the bench network, easiest first:"
echo "       ssh admin@raspberrypi.local        # mDNS; use the hostname set when flashing"
echo "       hostname -I                        # on the Pi itself, if you have a screen"
echo "       # or read the client list off the bench router"
echo ""
echo "Add your login user as a last argument if it is not admin:"
echo "       ./deploy-fms.sh 192.168.1.42 sam"
echo ""
echo "Useful service commands (run on any Pi):"
echo "  sudo systemctl status bioarena   # check it's running"
echo "  sudo journalctl -u bioarena -f   # tail live logs"
echo "  sudo systemctl restart bioarena  # restart after a new deploy"
echo ""
echo "Time service (every field). Nothing here has a battery-backed clock, so without it the"
echo "switch and the controller timestamp their logs years apart. Both halves are automatic:"
echo "deploy-fms.sh installs chrony and its drop-in, and bioarena points the switch at the Pi"
echo "as part of the baseline it pushes on its first configuration."
echo ""
echo "  Deploy once while the Pi still has internet, before wiring it into the field: apt is"
echo "  the one part that needs a route out, and the field network has none. If it cannot"
echo "  reach a mirror the deploy warns and carries on -- run it again once the Pi is online."
echo ""
echo "Network note:"
echo "  Main Pi:        10.0.100.5/24  (eth0, set by bioarena.service)"
echo "  Red panel Pi:   10.0.100.11/24 (eth0, set by estop-panel.service)"
echo "  Blue panel Pi:  10.0.100.12/24 (eth0, set by estop-panel.service)"
