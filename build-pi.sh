#!/usr/bin/env bash
# Cross-compile cheesy-arena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets the 32-bit Raspberry Pi OS (armv7l / armhf), which is the default
# OS image for most Pi 4 installations.  If your Pi runs the 64-bit image
# (uname -m returns aarch64) change GOARCH to arm64 and remove GOARM.
#
# Output: cheesy-arena-pi  (single static binary, copy to the Pi and run)

set -euo pipefail

OUTPUT="cheesy-arena-pi"

echo "Building for linux/arm (armv7 / 32-bit Raspberry Pi OS)..."
GOOS=linux GOARCH=arm GOARM=7 go build -o "$OUTPUT" .

echo "Done: $OUTPUT"
echo ""
echo "Deploy steps:"
echo "  1. Copy the binary and static assets to the Pi:"
echo "       scp $OUTPUT pi@<PI_IP>:~/cheesy-arena/"
echo "       scp -r static templates font schedules audio pi@<PI_IP>:~/cheesy-arena/"
echo ""
echo "  2. On the Pi, make the binary executable:"
echo "       chmod +x ~/cheesy-arena/$OUTPUT"
echo ""
echo "  3. Install the systemd service so it starts on boot:"
echo "       scp cheesy-arena.service pi@<PI_IP>:~/"
echo "       # then on the Pi:"
echo "       sudo mv ~/cheesy-arena.service /etc/systemd/system/"
echo "       sudo systemctl daemon-reload"
echo "       sudo systemctl enable cheesy-arena"
echo "       sudo systemctl start cheesy-arena"
echo ""
echo "  4. Access the web UI at http://<PI_IP>:8080"
echo ""
echo "Useful service commands (run on the Pi):"
echo "  sudo systemctl status cheesy-arena   # check it's running"
echo "  sudo journalctl -u cheesy-arena -f   # tail live logs"
echo "  sudo systemctl restart cheesy-arena  # restart after a new deploy"
echo ""
echo "Network note:"
echo "  The service assigns 10.0.100.5/24 to eth0 automatically on start."
echo "  Driver stations connect to that address on ports 1750/1120/1121."
