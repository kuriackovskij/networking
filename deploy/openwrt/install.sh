#!/bin/sh
# IP-Beamer installer for OpenWrt (run this ON the router, from the unpacked
# tarball directory). Idempotent: re-running upgrades the binary and init script
# and never overwrites an existing config.
set -e

BINDIR=/usr/bin
CONFDIR=/etc/ipbeam
INIT=/etc/init.d/ipbeamd
HOOKFILE=/etc/ipbeam/firewall.include
OLD_HOOK="/usr/bin/ipbeamd -config /etc/ipbeam/config.json -setup-firewall"

here=$(dirname "$0")
cd "$here"

# 1. binary
cp ipbeamd "$BINDIR/ipbeamd"
chmod 0755 "$BINDIR/ipbeamd"

# 2. config — keep any existing one; secure it either way (holds the secret)
mkdir -p "$CONFDIR"
if [ -f "$CONFDIR/config.json" ]; then
	echo "keeping existing $CONFDIR/config.json"
else
	cp config.json "$CONFDIR/config.json"
	echo "installed default $CONFDIR/config.json"
fi
chown root:root "$CONFDIR/config.json" 2>/dev/null || true
chmod 0600 "$CONFDIR/config.json"

# 3. init script + enable on boot
cp ipbeamd.init "$INIT"
chmod 0755 "$INIT"
"$INIT" enable

# stash the uninstaller so it's available later (run: sh /etc/ipbeam/uninstall.sh)
[ -f uninstall.sh ] && { cp uninstall.sh "$CONFDIR/uninstall.sh"; chmod 0755 "$CONFDIR/uninstall.sh"; }

# 4. re-apply the gate on every fw3 firewall reload.
#    fw3 flushes the iptables ruleset (incl. our mangle rule) on *reload* — which
#    also happens when the WAN/PPPoE link comes up after boot. A plain
#    /etc/firewall.user include runs only on *restart*, so the rule would be lost
#    on the first reload. We register a UCI include with `reload '1'` so it
#    re-runs on every reload too.
cat > "$HOOKFILE" <<EOF
#!/bin/sh
/usr/bin/ipbeamd -config /etc/ipbeam/config.json -setup-firewall
EOF
chmod 0755 "$HOOKFILE"

# migrate away from the old firewall.user line if a previous install added it
if [ -f /etc/firewall.user ] && grep -qF "$OLD_HOOK" /etc/firewall.user; then
	grep -vF "$OLD_HOOK" /etc/firewall.user > /etc/firewall.user.tmp && mv /etc/firewall.user.tmp /etc/firewall.user
fi

# add the UCI include once (idempotent)
if ! uci show firewall 2>/dev/null | grep -qF "$HOOKFILE"; then
	uci add firewall include >/dev/null
	uci set firewall.@include[-1].path="$HOOKFILE"
	uci set firewall.@include[-1].reload='1'
	uci commit firewall
	echo "registered firewall include (reload=1)"
fi

# 5. dependency check
if ! command -v ipset >/dev/null 2>&1; then
	echo "WARNING: 'ipset' not found. Install with:"
	echo "  opkg update && opkg install ipset iptables-mod-ipset kmod-ipt-ipset"
fi

cat <<EOF

ip-beamer installed. Next:
  1) vi $CONFDIR/config.json     # set password, wan_if, tcp_ports, udp_ports
  2) $INIT start
  3) /etc/init.d/firewall reload # applies the gate hook
EOF
