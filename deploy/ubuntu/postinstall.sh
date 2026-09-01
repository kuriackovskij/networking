#!/bin/sh
# Debian postinstall: secure the config, enable the service, and (on upgrade)
# restart it. On first install we do NOT start it, so the placeholder config
# isn't used — the admin edits it first.
set -e

CONF=/etc/ipbeam/config.json

if [ -f "$CONF" ]; then
	chown root:root "$CONF" || true
	chmod 600 "$CONF" || true
fi

systemctl daemon-reload || true
systemctl enable ipbeamd.service || true

# On a fresh install, $2 (the previously configured version) is empty.
if [ -z "$2" ]; then
	echo "ip-beamer installed. Next:"
	echo "  1) sudo vi $CONF        # set password, wan_if, tcp_ports, udp_ports"
	echo "  2) sudo systemctl start ipbeamd"
else
	if systemctl is-active --quiet ipbeamd.service; then
		systemctl restart ipbeamd.service || true
	fi
fi

exit 0
