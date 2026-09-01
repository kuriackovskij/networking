#!/bin/sh
# IP-Beamer installer for OpenWrt (run this ON the router, from the unpacked
# tarball directory). Idempotent: re-running upgrades the binary and init script
# and never overwrites an existing config.
set -e

BINDIR=/usr/bin
CONFDIR=/etc/ipbeam
INIT=/etc/init.d/ipbeamd
HOOK="/usr/bin/ipbeamd -config /etc/ipbeam/config.json -setup-firewall"

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

# 4. re-apply the gate on every fw3 firewall reload (fw3 flushes iptables)
touch /etc/firewall.user
if ! grep -qF "$HOOK" /etc/firewall.user; then
	echo "$HOOK" >> /etc/firewall.user
	echo "added firewall.user hook"
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
