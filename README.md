# IP-Beamer

Remotely open your firewall to just *your* current IP by sending one
authenticated UDP packet — "single packet authorization" (SPA). A client app
beams a signed packet to your edge box; the server verifies it and adds the
sender's public IP to a firewall allow-list for 60 minutes. No fresh beam, and
the IP expires on its own. This replaces coarse country-wide allow rules with a
list containing only the IPs of devices that have proven they know your
password.

## How it works

```
        YOUR DEVICE  (Android / iOS / Windows / macOS / CLI)
        ┌──────────────────────────────────────────────────┐
        │  password ─► PBKDF2(200k) ─► key                  │
        │  packet = magic | version | timestamp | nonce |   │
        │           node-name                               │
        │  sign: HMAC-SHA256(key, packet)                   │
        └───────────────────────┬──────────────────────────┘
                                 │  (1) UDP beam, ~80 bytes
                                 ▼
        ══════════════════════ INTERNET ══════════════════════
                                 │  source IP = your public IP
                                 ▼
   ┌──────────── EDGE FIREWALL  (Flint 2 / Ubuntu 24) ────────────────┐
   │                                                                  │
   │  ipbeamd  (listens UDP :62201)                                   │
   │   (2) verify HMAC ............ ✗ ─► drop silently (no reply)     │
   │   (3) timestamp within ±30s .. ✗ ─► reject                       │
   │   (4) nonce not seen before .. ✗ ─► reject (anti-replay)         │
   │   (5) ✓ add SOURCE IP to allow-list (ipset / nft), timeout 60m   │
   │   (6) send signed ACK ─────────────► client shows "granted <ip>" │
   │                                                                  │
   │  packet filter  (PREROUTING — runs BEFORE port-forward / DNAT)   │
   │   incoming to a gated port (:443, :8443, …):                     │
   │        source IP in allow-list ?                                 │
   │           ├─ yes ─► pass ─► DNAT / port-forward ─► LAN service   │
   │           └─ no  ─► DROP                                         │
   │                                                                  │
   │  (7) allow-list entry expires after 60m ─► access auto-closes    │
   │      (a fresh beam re-adds the IP and resets the 60m timer)      │
   └──────────────────────────────────────────────────────────────────┘
```

1. **Beam** — the app derives a key from your password and sends one signed UDP
   packet.
2–4. **Verify** — the daemon checks the signature, the timestamp, and the nonce.
   Any failure is dropped, most in total silence.
5. **Grant** — the sender's *observed* public IP is added to the allow-list with
   a 60-minute timeout.
6. **Confirm** — a signed acknowledgement lets the app show success.
7. **Expire** — with no fresh beam the entry removes itself; a repeat beam every
   ~45 min keeps it alive.

Meanwhile the packet filter, sitting *before* port-forwarding, lets only
allow-listed IPs reach the gated ports and drops everyone else.

## Why it's safe

- Every beam is signed with **HMAC-SHA256** over a key stretched from your
  password (PBKDF2, 200k iterations) — a captured packet is expensive to crack.
- A **timestamp + random nonce** stop capture-and-replay.
- The server whitelists the **source IP it actually sees**, so no one can open
  access for an address that isn't theirs.
- Unauthenticated packets get **no reply** — the port is invisible to scanners.
- The block runs **before port-forwarding (DNAT)**: rules are evaluated in
  PREROUTING, so a non-whitelisted packet is dropped before any forward can
  deliver it. A configured port-forward does *not* leak the service.

See [`PROTOCOL.md`](PROTOCOL.md) for the exact wire format and threat model.

## Multiple devices and ports

- **Many devices:** all your devices share the same password. Each one beams its
  own public IP, which becomes a separate member of the allow-list with its own
  60-minute timer — so every device that has beamed recently gets access at once.
  The node name in each beam is just a label for the logs. No per-device setup.
- **Which ports open:** set a **TCP list and a UDP list** in the config file
  (`tcp_ports` / `udp_ports`), applied to every whitelisted IP. These are the
  **external (public)** port numbers, matched pre-DNAT. Only the ports you list
  are gated; any other port you port-forward stays open to everyone, unchanged.
  For port-less protocols (ICMP, ESP, AH, GRE, …) use the `protocols` list.

## Server-side configuration

**Everything lives in one file: `/etc/ipbeam/config.json`.** Edit it and restart
the service to apply any change — the daemon installs the firewall sets and gate
rules itself from this file on start, so there is nothing else to touch.

| Key | Meaning | Default |
|-----|---------|---------|
| `password` | shared secret typed into every client | *(required)* |
| `backend` | `ipset` (Flint 2 / fw3) or `nft` (Ubuntu) | `ipset` |
| `wan_if` | WAN interface the gate applies to | `eth1` |
| `tcp_ports` | external TCP ports to gate (e.g. `[443, 8443]`) | `[443, 8443]` |
| `udp_ports` | external UDP ports to gate (e.g. `[51820]`) | `[]` |
| `protocols` | whole IP protocols to gate: `icmp`, `icmpv6`, `esp`, `ah`, `gre`, or a number | `[]` |
| `timeout` | how long a granted IP stays allowed | `60m` |
| `replay_window` | max clock skew / anti-replay window | `30s` |
| `listen` | UDP address\:port to listen on | `:62201` |
| `set4` / `set6` | allow-list set names (IPv4 / IPv6) | `spa_allow` / `spa_allow6` |
| `maxelem` | max whitelisted IPs (ipset backend) | `4096` |
| `nft_table` | nftables table name (nft backend) | `ipbeam` |
| `send_ack` | reply with a signed confirmation | `true` |
| `log_grants` / `log_rejects` | verbose logging switches | `false` |

Durations accept Go syntax: `s`, `m`, `h` (e.g. `90m`, `2h`, `45s`). The
`ipset` backend allows at most 15 ports per list (kernel `multiport` limit).

**Gating whole protocols.** `tcp_ports`/`udp_ports` gate specific ports; use
`protocols` for port-less protocols you want restricted to whitelisted IPs —
e.g. `"protocols": ["icmp", "esp"]` blocks ping and IPsec/ESP from everyone but
whitelisted sources. `tcp`/`udp` are rejected here (use the port lists). The
client never sends ICMP, so gating ICMP does not affect beaming. Two caveats:

- Gating `icmp` can interfere with **Path-MTU discovery** — if large transfers
  hang, allow ICMP type 3 (or leave `icmp` ungated).
- Gating `icmpv6` blocks IPv6 **Neighbor Discovery / Router Advertisement** on
  the WAN and will likely **break IPv6 connectivity** — only gate it if you
  don't use IPv6.

The file holds the password, so keep it **root-only**:
`sudo chown root:root /etc/ipbeam/config.json && sudo chmod 600 /etc/ipbeam/config.json`
(the daemon warns at startup if it is readable by anyone else).

### Applying changes / restart

Edit `/etc/ipbeam/config.json`, then:

```sh
# Ubuntu
sudo systemctl restart ipbeamd

# OpenWrt (Flint 2)
/etc/init.d/ipbeamd restart
```

Both are enabled at install time, so the service **auto-starts on boot** and
survives reboots. If the daemon is ever stopped, the gate rules remain in place
(fail-safe): no new IPs are granted and existing ones expire.

## Lightweight by design

- **Server:** a single ~3 MB static Go binary, no dependencies. It sleeps in a
  blocking UDP read (≈0% CPU idle) and wakes only to verify a packet and run one
  `ipset`/`nft` command. Fine for the Flint 2.
- **Client:** one UDP packet (<100 bytes), then done. To keep an IP alive, the
  app re-beams roughly every 45 min *only while you need access* — a single
  packet on a timer is negligible for battery.

## Repository layout

| Path | What |
|------|------|
| `internal/protocol` | wire format, HMAC, KDF (shared by server & client) |
| `cmd/ipbeamd` | server daemon (backends: `ipset` for fw3, `nft` for nftables) |
| `cmd/ipbeam` | reference CLI client (Linux/macOS/Windows) |
| `android/` | native Android client (Kotlin + Jetpack Compose) |
| `deploy/openwrt` | Flint 2: example config, procd init script, `install.sh` |
| `deploy/ubuntu` | Ubuntu: example config, systemd unit, deb maintainer scripts |
| `nfpm.yaml` | recipe for building the Ubuntu `.deb` |

## Build

```sh
git clone https://github.com/kuriackovskij/networking.git
cd networking
```

All artifacts land under `dist/` (`dist/server/`, `dist/client/`), with the
version in each filename.

```sh
make test                 # unit tests
make server-openwrt       # -> dist/server/ipbeamd-1.0.0-openwrt-arm64  (Flint 2)
make server-ubuntu        # -> dist/server/ipbeamd-1.0.0-ubuntu-amd64
make clients              # -> dist/client/ipbeam-1.0.0-{windows,macos,linux}-*

make deb                  # -> dist/server/ipbeamd_1.0.0_amd64.deb    (needs nfpm)
make openwrt-pkg          # -> dist/server/ipbeamd-1.0.0-openwrt-arm64.tar.gz
make packages             # deb + openwrt tarball + CLI clients
make android              # -> dist/client/ipbeamer-1.0.0.apk (needs Android SDK + JDK 17)
```

`make deb` needs [`nfpm`](https://nfpm.goreleaser.com/install/). Override the
version on any target, e.g. `make packages VERSION=1.1.0`.

## Install — Ubuntu 24.04 (`.deb`)

```sh
# build the package (dev machine), copy it over, install
make deb
scp dist/server/ipbeamd_1.0.0_amd64.deb user@server:/tmp/
ssh user@server
sudo apt install /tmp/ipbeamd_1.0.0_amd64.deb   # installs binary, unit, config; enables service

# edit the config and start
sudo vi /etc/ipbeam/config.json   # set password, wan_if, tcp_ports, udp_ports
sudo systemctl start ipbeamd
```

The package secures the config to `root:root 0600`, registers it as a conffile
(your edits survive upgrades), and enables the service on boot. It doesn't
auto-start on first install so the placeholder config is never used; on upgrades
it restarts a running service. If you run `ufw`, allow the beam UDP port
(`listen`, default 62201). Remove with `sudo apt remove ipbeamd`.

## Install — GL.iNet Flint 2 (OpenWrt 21.02) (tarball + installer)

```sh
# build the tarball (dev machine), copy it over
make openwrt-pkg
scp dist/server/ipbeamd-1.0.0-openwrt-arm64.tar.gz root@192.168.8.1:/tmp/

# on the router: unpack and run the installer
ssh root@192.168.8.1
mkdir -p /tmp/ipbeam && tar -C /tmp/ipbeam -xzf /tmp/ipbeamd-1.0.0-openwrt-arm64.tar.gz
sh /tmp/ipbeam/install.sh          # installs binary/config/init, enables on boot, adds fw3 hook

# edit the config and start
vi /etc/ipbeam/config.json         # set password, wan_if, tcp_ports, udp_ports
/etc/init.d/ipbeamd start
/etc/init.d/firewall reload        # applies the gate hook
```

The installer keeps any existing config, secures it to `0600`, enables the
service on boot, and adds the `firewall.user` hook so the gate survives fw3
reloads. If `ipset` is missing it tells you to
`opkg update && opkg install ipset iptables-mod-ipset kmod-ipt-ipset`. The
daemon opens the beam UDP port on the WAN itself; no extra rule needed.

## Use the CLI client

```sh
ipbeam -server myhome.example.com:62201 -password 'your passphrase' -node laptop
# -> beam sent to myhome.example.com:62201
# -> access granted for 203.0.113.42
```

## Logging (for troubleshooting)

The daemon is quiet by default. Two independent switches in `config.json`:

- `log_grants`: an `INFO` line per successful grant / keep-alive.
- `log_rejects`: a `WARN` line per rejected beam (bad password, stale, replay).

Firewall-command failures always log as `ERROR`. View logs with `logread -f`
(OpenWrt) or `journalctl -u ipbeamd -f` (Ubuntu).

## Clients

- **CLI** (`cmd/ipbeam`) — Linux/macOS/Windows, see above.
- **Android** (`android/`) — native Kotlin/Compose app: timed beams, status-bar
  icon, auto-start on boot, live acked/not-acked status, logs, encrypted
  password. Builds to an APK; see [`android/README.md`](android/README.md).

### Roadmap

Next native clients: **iOS → Windows → macOS**, each wrapping the same beam
logic with a server address + password and a background keep-alive timer.
