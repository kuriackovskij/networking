// Command ipbeamd is the IP-Beamer server daemon. It listens for authenticated
// UDP beams and, for each valid one, adds the sender's source IP to a firewall
// allow-list with a timeout. The list element carries its own timeout, so the
// address expires by itself when no fresh beam arrives — no cron, no cleanup.
//
// Two firewall backends are supported:
//   - "ipset":  for iptables/fw3 systems (e.g. OpenWrt 21.02 on the GL.iNet Flint 2)
//   - "nft":    for nftables systems (e.g. Ubuntu 24.04)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/kuriackovskij/networking/internal/protocol"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

// Routine logs (INFO/WARN) go to stdout; only genuine faults (ERROR) and fatals
// go to stderr. This keeps normal activity out of the "err" syslog level under
// both procd (OpenWrt) and journald/systemd (Ubuntu).
var (
	logOut = log.New(os.Stdout, "", log.LstdFlags)
	logErr = log.New(os.Stderr, "", log.LstdFlags)
)

// Config is the on-disk JSON configuration — the single source of truth. Edit
// it and restart the service to apply any change (interface, ports, password,
// timers, logging).
type Config struct {
	Listen       string   `json:"listen"`        // UDP listen address, e.g. ":62201"
	Password     string   `json:"password"`      // shared secret
	Backend      string   `json:"backend"`       // "ipset" or "nft"
	WanIf        string   `json:"wan_if"`        // WAN interface the gate applies to
	TCPPorts     []int    `json:"tcp_ports"`     // external TCP ports to gate
	UDPPorts     []int    `json:"udp_ports"`     // external UDP ports to gate
	Protocols    []string `json:"protocols"`     // whole IP protocols to gate: icmp, esp, ah, gre, or a number
	Set4         string   `json:"set4"`          // IPv4 set name
	Set6         string   `json:"set6"`          // IPv6 set name ("" to ignore IPv6 beams)
	Timeout      string   `json:"timeout"`       // element lifetime, e.g. "60m"
	ReplayWindow string   `json:"replay_window"` // max clock skew / replay window, e.g. "30s"
	MaxElem      int      `json:"maxelem"`       // max whitelisted IPs (ipset backend)
	SendAck      bool     `json:"send_ack"`      // reply with a signed confirmation

	// Logging is quiet by default; enable these for troubleshooting.
	LogGrants  bool `json:"log_grants"`  // INFO line for each successful grant/keep-alive
	LogRejects bool `json:"log_rejects"` // WARN line for each rejected/failed beam

	// nft backend only:
	NftTable string `json:"nft_table"` // table name, e.g. "ipbeam"
}

func defaultConfig() Config {
	return Config{
		Listen:       ":62201",
		Backend:      "ipset",
		WanIf:        "eth1",
		TCPPorts:     []int{443, 8443},
		UDPPorts:     []int{},
		Set4:         "spa_allow",
		Set6:         "spa_allow6",
		Timeout:      "60m",
		ReplayWindow: "30s",
		MaxElem:      4096,
		SendAck:      true,
		NftTable:     "ipbeam",
	}
}

// server holds the resolved runtime settings for a beam handler.
type server struct {
	cfg         Config
	key         []byte
	timeoutSecs int // for the ipset backend
	window      time.Duration
	nc          *nonceCache
}

func main() {
	cfgPath := flag.String("config", "/etc/ipbeam/config.json", "path to config file")
	setupOnly := flag.Bool("setup-firewall", false, "install the sets and gate rules from the config, then exit")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVer {
		fmt.Println("ipbeamd", version)
		return
	}

	cfg := defaultConfig()
	data, err := os.ReadFile(*cfgPath)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if cfg.Password == "" {
		log.Fatal("config: password is required")
	}
	if cfg.Backend != "ipset" && cfg.Backend != "nft" {
		log.Fatalf("config: backend must be \"ipset\" or \"nft\", got %q", cfg.Backend)
	}
	// The config holds the shared secret; warn loudly if anyone but the owner
	// can read it.
	if info, serr := os.Stat(*cfgPath); serr == nil && info.Mode().Perm()&0o077 != 0 {
		logOut.Printf("WARN config %s is accessible beyond its owner (mode %04o) but holds the password; run: chown root:root %s && chmod 600 %s",
			*cfgPath, info.Mode().Perm(), *cfgPath, *cfgPath)
	}

	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		log.Fatalf("config: bad timeout: %v", err)
	}
	window, err := time.ParseDuration(cfg.ReplayWindow)
	if err != nil {
		log.Fatalf("config: bad replay_window: %v", err)
	}

	// Install the allow-list set(s) and the gate rules from the config. This is
	// idempotent and preserves existing members, so it is safe on every start.
	if err := ensureFirewall(cfg, int(timeout.Seconds())); err != nil {
		log.Fatalf("firewall setup: %v", err)
	}
	if *setupOnly {
		logOut.Print("INFO firewall rules installed; exiting (-setup-firewall)")
		return
	}

	srv := &server{
		cfg:         cfg,
		key:         protocol.DeriveKey(cfg.Password),
		timeoutSecs: int(timeout.Seconds()),
		window:      window,
		nc:          newNonceCache(2 * window),
	}
	go srv.nc.gc()

	addr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		log.Fatalf("resolve listen addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	if cfg.WanIf != "" {
		if _, ierr := net.InterfaceByName(cfg.WanIf); ierr != nil {
			logOut.Printf("WARN wan_if %q is not present yet (%v); the gate matches nothing until it appears — verify with `ip route show default` (PPPoE is usually \"pppoe-wan\")",
				cfg.WanIf, ierr)
		}
	}
	logOut.Printf("INFO ipbeamd %s listening on %s (backend %s, wan %s, timeout %s; log_grants=%t log_rejects=%t)",
		version, cfg.Listen, cfg.Backend, cfg.WanIf, cfg.Timeout, cfg.LogGrants, cfg.LogRejects)

	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			logErr.Printf("ERROR read: %v", err)
			continue
		}
		msg := make([]byte, n)
		copy(msg, buf[:n])
		go srv.handle(conn, src, msg)
	}
}

func (s *server) handle(conn *net.UDPConn, src *net.UDPAddr, data []byte) {
	pkt, err := protocol.Decode(data, s.key)
	if err != nil {
		// Never answer an unauthenticated packet — that keeps the port
		// invisible to scanners. Optionally record the failed attempt.
		s.reject("rejected beam from %s: %v", src.IP, err)
		return
	}
	if skew := abs(time.Since(pkt.Timestamp)); skew > s.window {
		s.reject("rejected beam from %s (node %q): stale timestamp (%v)", src.IP, pkt.NodeName, skew)
		return
	}
	if !s.nc.check(pkt.Nonce) {
		s.reject("rejected beam from %s (node %q): replayed nonce", src.IP, pkt.NodeName)
		return
	}

	ip := src.IP
	// A beam whose source is a private/loopback/link-local address (e.g. a
	// client on the LAN) doesn't need whitelisting — the WAN gate wouldn't apply
	// to it anyway. Skip it and note it, so the allow-list only ever holds
	// routable public addresses.
	if isNonPublic(ip) {
		s.info("ignoring beam from non-public source %s (node %q) — not added to allow-list", ip, pkt.NodeName)
		return
	}
	set := s.cfg.Set4
	if ip.To4() == nil {
		set = s.cfg.Set6
	}
	if set == "" {
		s.reject("rejected beam from %s (node %q): no set for this address family", ip, pkt.NodeName)
		return
	}
	if err := s.addElement(set, ip); err != nil {
		// A firewall command failing is an operational fault, always logged.
		logErr.Printf("ERROR add %s -> %s failed: %v", ip, set, err)
		return
	}
	s.grant("granted %s (node %q) for %s", ip, pkt.NodeName, s.cfg.Timeout)

	if s.cfg.SendAck {
		if ack, err := protocol.EncodeAck(pkt.Nonce, ip.String(), s.key); err == nil {
			conn.WriteToUDP(ack, src)
		}
	}
}

// addElement adds (or refreshes) an IP in the allow-list with the configured
// timeout. Re-adding an existing element resets its timer, which is exactly the
// keep-alive behaviour we want from repeated beams.
func (s *server) addElement(set string, ip net.IP) error {
	switch s.cfg.Backend {
	case "ipset":
		// ipset timeouts are integer seconds; -exist updates an existing member.
		return run("ipset", "add", set, ip.String(),
			"timeout", strconv.Itoa(s.timeoutSecs), "-exist")
	case "nft":
		elem := fmt.Sprintf("{ %s timeout %s }", ip, s.cfg.Timeout)
		return run("nft", "add", "element", "inet", s.cfg.NftTable, set, elem)
	default:
		return fmt.Errorf("unknown backend %q", s.cfg.Backend)
	}
}

// grant logs a successful beam at INFO, only when log_grants is enabled.
func (s *server) grant(format string, args ...any) {
	if s.cfg.LogGrants {
		logOut.Printf("INFO "+format, args...)
	}
}

// info logs an informational note at INFO, only when log_grants is enabled.
func (s *server) info(format string, args ...any) {
	if s.cfg.LogGrants {
		logOut.Printf("INFO "+format, args...)
	}
}

// isNonPublic reports whether ip is an RFC1918/ULA private, loopback,
// link-local, or unspecified address — none of which belong in the WAN
// allow-list.
func isNonPublic(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// reject logs a failed/dropped beam at WARN, only when log_rejects is enabled.
func (s *server) reject(format string, args ...any) {
	if s.cfg.LogRejects {
		logOut.Printf("WARN "+format, args...)
	}
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, out)
	}
	return nil
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// nonceCache remembers recently seen nonces to defeat replay within the window.
type nonceCache struct {
	mu   sync.Mutex
	seen map[[protocol.NonceLen]byte]time.Time
	ttl  time.Duration
}

func newNonceCache(ttl time.Duration) *nonceCache {
	return &nonceCache{seen: make(map[[protocol.NonceLen]byte]time.Time), ttl: ttl}
}

// check records the nonce and reports whether it was fresh (not seen within ttl).
func (c *nonceCache) check(n [protocol.NonceLen]byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if t, ok := c.seen[n]; ok && now.Sub(t) < c.ttl {
		return false
	}
	c.seen[n] = now
	return true
}

func (c *nonceCache) gc() {
	for {
		time.Sleep(c.ttl)
		c.mu.Lock()
		now := time.Now()
		for k, t := range c.seen {
			if now.Sub(t) >= c.ttl {
				delete(c.seen, k)
			}
		}
		c.mu.Unlock()
	}
}
