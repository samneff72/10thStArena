#!/usr/bin/env python3
"""Bring a factory or reset Catalyst 3560-CX to the point bioarena can configure it.

Bioarena applies the field's standing configuration itself -- VLANs, station and trunk
ports, routing -- but it can only do that over Telnet, and Telnet needs an address and a
password that do not exist on a switch out of the box. That bootstrap is console-only by
definition. This script does it, so nobody composes IOS by hand:

    python3 switch-bootstrap.py --password fieldpassword

Sets the hostname, the management address, the enable and VTY passwords, enables Telnet,
and points the boot loader at the installed image so the switch stops stopping at the
"switch:" prompt. Then saves.

Uses only the Python standard library, and Linux or macOS only, for the same reasons as
console.py -- run it from the Pi.

Everything here is idempotent: running it twice changes nothing the second time.
"""

import argparse
import os
import re
import select
import sys
import time

try:
    import termios
except ImportError:  # pragma: no cover - platform guard
    sys.exit(
        "switch-bootstrap.py needs a POSIX terminal (termios), so it does not run on\n"
        "Windows. Run it from the Pi, which is on the field with the switch anyway."
    )

DEFAULT_ADDRESS = "10.0.100.3"
DEFAULT_MASK = "255.255.255.0"
DEFAULT_HOSTNAME = "bioSwitch"
READ_TIMEOUT_SEC = 5

# "dir /recursive" walks the whole flash, and "write memory" erases and rewrites it,
# answering with progress dots for as long as it takes. Both routinely outlast the default.
DIR_TIMEOUT_SEC = 30
WRITE_TIMEOUT_SEC = 60


def open_port(device, baud=9600):
    """Open the console at 9600 8N1, no flow control -- Cisco's console defaults."""
    fd = os.open(device, os.O_RDWR | os.O_NOCTTY)
    iflag, oflag, cflag, lflag, _, _, cc = termios.tcgetattr(fd)
    cflag |= termios.CLOCAL | termios.CREAD
    cflag &= ~(termios.PARENB | termios.CSTOPB | termios.CSIZE)
    cflag |= termios.CS8
    crtscts = getattr(termios, "CRTSCTS", 0)
    if crtscts:
        cflag &= ~crtscts
    iflag = oflag = lflag = 0
    cc = list(cc)
    cc[termios.VMIN] = 0
    cc[termios.VTIME] = 0
    speed = getattr(termios, "B%d" % baud)
    termios.tcsetattr(fd, termios.TCSANOW, [iflag, oflag, cflag, lflag, speed, speed, cc])
    return fd


def read_until_idle(fd, idle_sec=0.6, timeout_sec=READ_TIMEOUT_SEC):
    """Collect output until the switch stops talking.

    Waiting for a specific prompt is unreliable here: the prompt changes as the
    configuration proceeds, an unconfigured switch may be offering its setup dialog, and
    "write memory" answers with progress dots. Idleness is the one signal that means the
    same thing in every state.

    The timeout is a backstop for a switch that never stops talking, not a budget for how
    long a command may take -- a command that hits it returns truncated output, which for
    "dir" means not finding the image and skipping the boot setting without saying so.
    Slow commands are given their own.
    """
    output = ""
    deadline = time.time() + timeout_sec
    last_data = time.time()
    while time.time() < deadline:
        readable, _, _ = select.select([fd], [], [], 0.1)
        if readable:
            chunk = os.read(fd, 4096)
            if chunk:
                output += chunk.decode("utf-8", errors="replace")
                last_data = time.time()
                continue
        if time.time() - last_data >= idle_sec:
            return output
    print("  WARNING: the switch was still talking after %ds; output may be truncated." % timeout_sec)
    return output


# IOS reports a rejected command in its output and carries on, so nothing fails visibly
# unless the output is read. These are the openings of its complaints.
ERROR_MARKERS = (
    "% Invalid input",
    "% Incomplete command",
    "% Ambiguous command",
    "% Unknown command",
    "% Bad",
    "% Error",
)


def report_errors(line, output):
    """Print any complaint the switch made about a command, and report whether it did."""
    failed = False
    for reply in output.splitlines():
        reply = reply.strip()
        if any(reply.startswith(marker) for marker in ERROR_MARKERS):
            print("  REJECTED: %s" % (line if line else "<enter>"))
            print("            %s" % reply)
            failed = True
    return failed


def send(fd, line, echo=True, timeout_sec=READ_TIMEOUT_SEC):
    os.write(fd, (line + "\r").encode())
    output = read_until_idle(fd, timeout_sec=timeout_sec)
    if echo and line:
        print("  %s" % line)
    if report_errors(line, output):
        # Not fatal: a switch that rejects one line usually accepts the rest, and stopping
        # here would leave it half configured with no record of how far it got. The count
        # is reported at the end so a bootstrap that did not fully take says so.
        send.rejections += 1
    return output


send.rejections = 0


def find_boot_image(fd):
    """Locate the IOS image so the switch boots on its own.

    A switch whose BOOT variable is unset stops at the boot loader on every power cycle,
    which reads as a dead switch on the morning of a practice session.
    """
    # Slow on a full flash, and truncating it means silently skipping the boot setting --
    # which surfaces as a switch that stops at the boot loader on some later power cycle.
    output = send(fd, "dir /recursive flash:", echo=False, timeout_sec=DIR_TIMEOUT_SEC)
    return parse_boot_image(output)


def parse_boot_image(output):
    """Find the IOS image in "dir /recursive flash:" output, with its directory.

    The image is usually inside a directory named for it, and the listing names that
    directory in a header line rather than beside the file:

        Directory of flash:/c3560cx-universalk9-mz.152-7.E/
           3  -rwx  24582144  Jun 2 2004  c3560cx-universalk9-mz.152-7.E.bin

    Taking the filename alone yields a path that does not exist, and "boot system" accepts
    it without complaint until the next reboot stops at the boot loader.
    """
    directory = ""
    for line in output.splitlines():
        header = re.search(r"Directory of flash:/?(\S*)", line)
        if header:
            directory = header.group(1).strip("/")
            continue
        image = re.search(r"(\S+\.bin)\b", line)
        if image:
            name = image.group(1)
            return "%s/%s" % (directory, name) if directory else name
    return None


def bootstrap(fd, args):
    print("Waking the console...")
    send(fd, "", echo=False)
    output = send(fd, "", echo=False)

    # A switch with no configuration offers its setup dialog first; decline it, since
    # everything it would ask is set below.
    if "initial configuration dialog" in output:
        print("Declining the setup dialog.")
        send(fd, "no", echo=False)
        send(fd, "", echo=False)

    print("Entering privileged mode...")
    output = send(fd, "enable", echo=False)
    if "Password:" in output:
        send(fd, args.password, echo=False)

    image = find_boot_image(fd)
    if image:
        print("Found IOS image: %s" % image)
    else:
        print("WARNING: no .bin image found in flash; leaving the boot setting alone.")
        print("         The switch may stop at the 'switch:' prompt on the next reboot.")

    print("Applying bootstrap configuration:")
    send(fd, "configure terminal", echo=False)
    for line in [
        "hostname %s" % args.hostname,
        "interface Vlan1",
        "ip address %s %s" % (args.address, args.mask),
        "no shutdown",
        "exit",
        "enable secret %s" % args.password,
        "line vty 0 4",
        "password %s" % args.password,
        "login",
        "transport input telnet",
        "exit",
        "line vty 5 15",
        "transport input none",
        "exit",
        "service password-encryption",
    ] + (["boot system flash:%s" % image] if image else []):
        # The passwords are the point of the exercise, so they are not echoed.
        send(fd, line, echo="password" not in line and "secret" not in line)
    send(fd, "end", echo=False)

    print("Saving...")
    send(fd, "write memory", echo=False, timeout_sec=WRITE_TIMEOUT_SEC)

    print("")
    if send.rejections:
        print("%d command(s) were rejected -- see REJECTED above." % send.rejections)
        print("The switch is partly configured. Fix the cause and run this again; it is")
        print("safe to repeat.")
        return 1

    print("Done. The switch answers Telnet at %s." % args.address)
    print("Enter that address and the password under Arena > Settings; bioarena applies")
    print("the VLANs, station ports, trunks and routing itself on the next match load.")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--device", default="/dev/ttyUSB0", help="serial device (default: /dev/ttyUSB0)")
    parser.add_argument("--address", default=DEFAULT_ADDRESS, help="management address (default: %s)" % DEFAULT_ADDRESS)
    parser.add_argument("--mask", default=DEFAULT_MASK, help="management subnet mask")
    parser.add_argument("--hostname", default=DEFAULT_HOSTNAME, help="switch hostname, ideally naming the site")
    parser.add_argument(
        "--password",
        required=True,
        help="enable and VTY password. Bioarena sends one password for both, so they must match.",
    )
    args = parser.parse_args()

    try:
        fd = open_port(args.device)
    except FileNotFoundError:
        sys.exit("%s does not exist. Check the console cable, then: dmesg | tail" % args.device)
    except PermissionError:
        sys.exit(
            "Permission denied opening %s.\n"
            "  Run with sudo, or add yourself to the dialout group and log in again." % args.device
        )

    try:
        sys.exit(bootstrap(fd, args))
    finally:
        os.close(fd)


if __name__ == "__main__":
    main()
