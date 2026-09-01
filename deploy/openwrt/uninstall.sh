#!/bin/sh
# IP-Beamer uninstaller for OpenWrt. Stops the service, removes the binary, init
# script and firewall hook, tears down the gate rules, and destroys the
# allow-list sets. Pass --purge to also delete the config (holds the password).
set -e

PURGE=0
[ "$1" = "--purge" ] && PURGE=1

INIT=/etc/init.d/ipbeamd
HOOKFILE=/etc/ipbeam/firewall.include
OLD_HOOK='/usr/bin/ipbeamd -config /etc/ipbeam/config.json -setup-firewall'
SET4="spa_allow"
SET6="spa_allow6"

# 1. stop + disable + remove service files
[ -x "$INIT" ] && { "$INIT" stop 2>/dev/null; "$INIT" disable 2>/dev/null; }
rm -f "$INIT" /usr/bin/ipbeamd

# 2. remove our firewall hooks, then reload fw3 so it rebuilds the ruleset
#    WITHOUT our mangle/INPUT rules (they are not persisted by fw3).
#    a) the UCI include (new installs)
sec=$(uci show firewall 2>/dev/null | sed -n "s|^firewall\.\(@include\[[0-9]*\]\)\.path='$HOOKFILE'.*|\1|p" | head -1)
if [ -n "$sec" ]; then
	uci -q delete "firewall.$sec"
	uci commit firewall
fi
rm -f "$HOOKFILE"
#    b) the legacy firewall.user line (older installs)
if [ -f /etc/firewall.user ] && grep -qF "$OLD_HOOK" /etc/firewall.user; then
	grep -vF "$OLD_HOOK" /etc/firewall.user > /etc/firewall.user.tmp && mv /etc/firewall.user.tmp /etc/firewall.user
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
