#!/bin/sh
# Debian preremove: stop the service and tear down the firewall gate when the
# package is removed, so the gated ports return to normal (the daemon is gone,
# so there is nothing left to keep the allow-list fresh). Deleting the table
# removes the sets and the gate chain in one step.
set -e

if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
	systemctl stop ipbeamd.service || true
	systemctl disable ipbeamd.service || true
	nft delete table inet ipbeam 2>/dev/null || true
fi

exit 0
