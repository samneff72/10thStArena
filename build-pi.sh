#!/usr/bin/env bash
# Cross-compile cheesy-arena for Raspberry Pi 4 (arm64 Linux).
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Output: cheesy-arena-pi  (single static binary, copy to the Pi and run)

set -euo pipefail

OUTPUT="cheesy-arena-pi"

echo "Building for linux/arm64..."
GOOS=linux GOARCH=arm64 go build -o "$OUTPUT" .

echo "Done: $OUTPUT"
echo ""
echo "Deploy steps:"
echo "  1. Copy the binary and static assets to the Pi:"
echo "       scp $OUTPUT pi@<PI_IP>:~/cheesy-arena/"
echo "       scp -r static templates font schedules audio pi@<PI_IP>:~/cheesy-arena/"
echo "  2. On the Pi, give the binary execute permission and run:"
echo "       chmod +x ~/cheesy-arena/$OUTPUT"
echo "       cd ~/cheesy-arena && ./$OUTPUT"
echo "  3. Access the web UI at http://<PI_IP>:8080"
echo ""
echo "Network note:"
echo "  The driver station connects to IP 10.0.100.5 on port 1750/1120/1121."
echo "  Assign that address to the Pi's ethernet interface before starting:"
echo "    sudo ip addr add 10.0.100.5/24 dev eth0"
