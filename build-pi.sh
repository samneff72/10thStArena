#!/usr/bin/env bash
# Cross-compile bioarena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets the 32-bit Raspberry Pi OS (armv7l / armhf), which is the default
# OS image for most Pi 4 installations.  If your Pi runs the 64-bit image
# (uname -m returns aarch64) change GOARCH to arm64 and remove GOARM.
#
# Output: bioarena  (single static binary, copy to the Pi and run)

set -euo pipefail

OUTPUT="bioarena"
PANEL_OUTPUT="estop-panel"

echo "Building bioarena for linux/arm (armv7 / 32-bit Raspberry Pi OS)..."
GOOS=linux GOARCH=arm GOARM=7 go build -o "$OUTPUT" .

echo "Building estop-panel for linux/arm..."
GOOS=linux GOARCH=arm GOARM=7 go build -o "$PANEL_OUTPUT" ./cmd/estop-panel

echo "Done: $OUTPUT  $PANEL_OUTPUT"
echo ""
echo "Deploy steps — main field controller Pi:"
echo "  1. Copy the binary and static assets to the Pi:"
echo "       scp $OUTPUT pi@<PI_IP>:~/bioarena/"
echo "       scp -r static templates font schedules audio pi@<PI_IP>:~/bioarena/"
echo ""
echo "  2. On the Pi, make the binary executable:"
echo "       chmod +x ~/bioarena/$OUTPUT"
echo ""
echo "  3. Install the systemd service so it starts on boot:"
echo "       scp bioarena.service pi@<PI_IP>:~/"
echo "       # then on the Pi:"
echo "       sudo mv ~/bioarena.service /etc/systemd/system/"
echo "       sudo systemctl daemon-reload"
echo "       sudo systemctl enable bioarena"
echo "       sudo systemctl start bioarena"
echo ""
echo "  4. Access the web UI at http://<PI_IP>:8080"
echo ""
echo "Deploy steps — e-stop panel Pi (repeat for red and blue):"
echo "  1. Copy the panel binary and config to the panel Pi:"
echo "       scp $PANEL_OUTPUT pi@<PANEL_PI_IP>:~/estop-panel/"
echo "       scp estop-panel.yaml pi@<PANEL_PI_IP>:~/estop-panel/"
echo ""
echo "  2. Make it executable:"
echo "       chmod +x ~/estop-panel/$PANEL_OUTPUT"
echo ""
echo "  3. Install the systemd service (edit IP in service file first):"
echo "       scp cmd/estop-panel/estop-panel.service pi@<PANEL_PI_IP>:~/"
echo "       # then on the panel Pi:"
echo "       sudo mv ~/estop-panel.service /etc/systemd/system/"
echo "       sudo systemctl daemon-reload"
echo "       sudo systemctl enable estop-panel"
echo "       sudo systemctl start estop-panel"
echo ""
echo "Useful service commands (run on any Pi):"
echo "  sudo systemctl status bioarena   # check it's running"
echo "  sudo journalctl -u bioarena -f   # tail live logs"
echo "  sudo systemctl restart bioarena  # restart after a new deploy"
echo ""
echo "Network note:"
echo "  Main Pi:        10.0.100.5/24  (eth0, set by bioarena.service)"
echo "  Red panel Pi:   10.0.100.11/24 (eth0, set by estop-panel.service)"
echo "  Blue panel Pi:  10.0.100.12/24 (eth0, set by estop-panel.service)"
