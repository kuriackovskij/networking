#!/bin/sh
# IP-Beamer uninstaller for OpenWrt. Stops the service, removes the binary, init
# script and firewall hook, tears down the gate rules, and destroys the
# allow-list sets. Pass --purge to also delete the config (holds the password).
set -e

PURGE=0
[ "$1" = "--purge" ] && PURGE=1

INIT=/etc/init.d/ipbeamd
HOOK='/usr/bin/ipbeamd -config /etc/ipbeam/config.json -setup-firewall'
SET4="spa_allow"
SET6="spa_allow6"

# 1. stop + disable + remove service files
[ -x "$INIT" ] && { "$INIT" stop 2>/dev/null; "$INIT" disable 2>/dev/null; }
rm -f "$INIT" /usr/bin/ipbeamd

# 2. remove the firewall.user hook line, then reload fw3 so it rebuilds the
#    ruleset WITHOUT our mangle/INPUT rules (they are not persisted by fw3).
if [ -f /etc/firewall.user ]; then
	grep -vF "$HOOK" /etc/firewall.user > /etc/firewall.user.tmp 2>/dev/null || true
	mv /etc/firewall.user.tmp /etc/firewall.user
fi
/etc/init.d/firewall reload 2>/dev/null || true

# 3. destroy the allow-list sets (now unreferenced)
ipset destroy "$SET4" 2>/dev/null || true
ipset destroy "$SET6" 2>/dev/null || true

# 4. config
if [ "$PURGE" = "1" ]; then
	rm -rf /etc/ipbeam
	echo "removed /etc/ipbeam (purged)"
else
	echo "kept /etc/ipbeam/config.json (run with --purge to remove it)"
fi

echo "ip-beamer uninstalled."
