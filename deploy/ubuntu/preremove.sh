#!/bin/sh
# Debian preremove: stop and disable the service when the package is removed.
# Note: the firewall gate rules the daemon installed are left in place so ports
# stay closed (fail-safe). To clear them: `sudo nft delete table inet ipbeam`.
set -e

if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
	systemctl stop ipbeamd.service || true
	systemctl disable ipbeamd.service || true
fi

exit 0
