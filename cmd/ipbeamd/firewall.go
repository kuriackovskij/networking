package main

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// ensureFirewall installs everything the config describes: the allow-list
// set(s) and the gate rules that drop non-whitelisted traffic to the gated
// ports and protocols on the WAN interface. It is idempotent and never flushes
// existing allow-list members, so it is safe to run on every service start or
// firewall reload. It deliberately does NOT provide a teardown: if the daemon
// stops, the gate stays closed (fail-safe).
func ensureFirewall(cfg Config, timeoutSecs int) error {
	if cfg.gatesAnything() && cfg.WanIf == "" {
		return fmt.Errorf("wan_if must be set when tcp_ports/udp_ports/protocols are configured")
	}
	// Validate protocol tokens up front so a typo fails loudly.
	if _, err := cfg.protoRules(); err != nil {
		return err
	}
	switch cfg.Backend {
	case "ipset":
		return ensureIpset(cfg, timeoutSecs)
	case "nft":
		return ensureNft(cfg)
	default:
		return fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}

func (c Config) gatesAnything() bool {
	return len(c.TCPPorts)+len(c.UDPPorts)+len(c.Protocols) > 0
}

// ---- ipset / iptables backend (OpenWrt 21.02 / fw3) ----------------------

func ensureIpset(cfg Config, timeoutSecs int) error {
	secs := strconv.Itoa(timeoutSecs)
	maxelem := strconv.Itoa(cfg.MaxElem)

	// Create the sets. -exist makes this a no-op (no flush) when they exist.
	if err := run("ipset", "create", cfg.Set4, "hash:ip",
		"timeout", secs, "maxelem", maxelem, "-exist"); err != nil {
		return err
	}
	if cfg.Set6 != "" {
		if err := run("ipset", "create", cfg.Set6, "hash:ip", "family", "inet6",
			"timeout", secs, "maxelem", maxelem, "-exist"); err != nil {
			return err
		}
	}

	// Port gates (pre-DNAT in mangle PREROUTING — see PROTOCOL.md).
	for _, p := range []struct {
		name  string
		ports []int
	}{{"tcp", cfg.TCPPorts}, {"udp", cfg.UDPPorts}} {
		if len(p.ports) == 0 {
			continue
		}
		if len(p.ports) > 15 {
			return fmt.Errorf("ipset backend: at most 15 %s ports (multiport limit), got %d", p.name, len(p.ports))
		}
		match := []string{"-p", p.name, "-m", "multiport", "--dports", portsCSV(p.ports)}
		if cfg.Set4 != "" {
			if err := mangleDrop("iptables", cfg.WanIf, match, cfg.Set4); err != nil {
				return err
			}
		}
		if cfg.Set6 != "" {
			if err := mangleDrop("ip6tables", cfg.WanIf, match, cfg.Set6); err != nil {
				return err
			}
		}
	}

	// Whole-protocol gates (ICMP, ESP, AH, GRE, numeric …).
	protos, _ := cfg.protoRules()
	for _, pr := range protos {
		if pr.v4 && cfg.Set4 != "" && pr.iptV4 != "" {
			if err := mangleDrop("iptables", cfg.WanIf, []string{"-p", pr.iptV4}, cfg.Set4); err != nil {
				return err
			}
		}
		if pr.v6 && cfg.Set6 != "" && pr.iptV6 != "" {
			if err := mangleDrop("ip6tables", cfg.WanIf, []string{"-p", pr.iptV6}, cfg.Set6); err != nil {
				return err
			}
		}
	}

	// fw3 default-drops WAN input, so open the beam listener port ourselves.
	// Inserting at the top of INPUT wins over the fw3 zone rules.
	if _, port, err := net.SplitHostPort(cfg.Listen); err == nil {
		for _, cmd := range []string{"iptables", "ip6tables"} {
			base := []string{"INPUT", "-i", cfg.WanIf, "-p", "udp", "--dport", port, "-j", "ACCEPT"}
			runIgnore(cmd, append([]string{"-D"}, base...)...)
			runIgnore(cmd, append([]string{"-I"}, base...)...)
		}
	}
	return nil
}

// mangleDrop inserts (idempotently) a pre-DNAT rule that drops WAN packets
// matching `match` whose source is NOT in `set`.
func mangleDrop(cmd, wan string, match []string, set string) error {
	base := []string{"-t", "mangle", "PREROUTING", "-i", wan}
	base = append(base, match...)
	base = append(base, "-m", "set", "!", "--match-set", set, "src", "-j", "DROP")
	runIgnore(cmd, replaceOp(base, "-D")...)
	return run(cmd, replaceOp(base, "-I")...)
}

// replaceOp inserts the chain-op flag (-I/-D) right before the "PREROUTING"
// token so the same argument slice can be used to both delete and insert.
func replaceOp(args []string, op string) []string {
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "PREROUTING" {
			out = append(out, op)
		}
		out = append(out, a)
	}
	return out
}

// ---- nft backend (Ubuntu 24.04 / nftables) -------------------------------

func ensureNft(cfg Config) error {
	t := cfg.NftTable

	// Create the table, sets and gate chain only if the table is absent, so a
	// reload never flushes existing allow-list members.
	if run("nft", "list", "table", "inet", t) != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "table inet %s {\n", t)
		fmt.Fprintf(&b, "  set %s { type ipv4_addr; flags timeout; }\n", cfg.Set4)
		if cfg.Set6 != "" {
			fmt.Fprintf(&b, "  set %s { type ipv6_addr; flags timeout; }\n", cfg.Set6)
		}
		b.WriteString("  chain gate { type filter hook prerouting priority -150; policy accept; }\n")
		b.WriteString("}\n")
		if err := runStdin("nft", []string{"-f", "-"}, b.String()); err != nil {
			return err
		}
	}

	// (Re)apply the gate rules from config without touching the sets.
	if err := run("nft", "flush", "chain", "inet", t, "gate"); err != nil {
		return err
	}
	rules, _ := cfg.nftGateRules()
	for _, rule := range rules {
		if err := run("nft", "add", "rule", "inet", t, "gate", rule); err != nil {
			return err
		}
	}
	return nil
}

// nftGateRules builds one drop rule per port/protocol and address-family.
func (cfg Config) nftGateRules() ([]string, error) {
	var rules []string
	// Ports.
	for _, p := range []struct {
		name  string
		ports []int
	}{{"tcp", cfg.TCPPorts}, {"udp", cfg.UDPPorts}} {
		if len(p.ports) == 0 {
			continue
		}
		spaced := portsSpaced(p.ports)
		if cfg.Set4 != "" {
			rules = append(rules, fmt.Sprintf("iifname %s %s dport { %s } ip saddr != @%s drop",
				cfg.WanIf, p.name, spaced, cfg.Set4))
		}
		if cfg.Set6 != "" {
			rules = append(rules, fmt.Sprintf("iifname %s %s dport { %s } ip6 saddr != @%s drop",
				cfg.WanIf, p.name, spaced, cfg.Set6))
		}
	}
	// Whole protocols.
	protos, err := cfg.protoRules()
	if err != nil {
		return nil, err
	}
	for _, pr := range protos {
		if pr.v4 && cfg.Set4 != "" {
			rules = append(rules, fmt.Sprintf("iifname %s meta l4proto %s ip saddr != @%s drop",
				cfg.WanIf, pr.nft, cfg.Set4))
		}
		if pr.v6 && cfg.Set6 != "" {
			rules = append(rules, fmt.Sprintf("iifname %s meta l4proto %s ip6 saddr != @%s drop",
				cfg.WanIf, pr.nft, cfg.Set6))
		}
	}
	return rules, nil
}

// ---- protocol parsing ----------------------------------------------------

// protoRule maps a configured protocol token to the per-backend, per-family
// values needed to build a gate rule.
type protoRule struct {
	iptV4 string // iptables -p value (v4); "" = not applicable
	iptV6 string // ip6tables -p value (v6); "" = not applicable
	nft   string // nft "meta l4proto" token
	v4    bool
	v6    bool
}

// protoRules parses cfg.Protocols. tcp/udp are rejected (use the port lists).
func (cfg Config) protoRules() ([]protoRule, error) {
	out := make([]protoRule, 0, len(cfg.Protocols))
	for _, tok := range cfg.Protocols {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "tcp", "6", "udp", "17":
			return nil, fmt.Errorf("protocol %q must be gated via tcp_ports/udp_ports, not \"protocols\"", tok)
		case "icmp", "1":
			out = append(out, protoRule{iptV4: "icmp", nft: "icmp", v4: true})
		case "icmpv6", "ipv6-icmp", "icmp6", "58":
			out = append(out, protoRule{iptV6: "icmpv6", nft: "icmpv6", v6: true})
		case "esp", "50":
			out = append(out, protoRule{iptV4: "esp", iptV6: "esp", nft: "esp", v4: true, v6: true})
		case "ah", "51":
			out = append(out, protoRule{iptV4: "ah", iptV6: "ah", nft: "ah", v4: true, v6: true})
		case "gre", "47":
			out = append(out, protoRule{iptV4: "gre", iptV6: "gre", nft: "gre", v4: true, v6: true})
		default:
			n, err := strconv.Atoi(strings.TrimSpace(tok))
			if err != nil || n < 0 || n > 255 {
				return nil, fmt.Errorf("unknown protocol %q (use a name or an IP protocol number 0-255)", tok)
			}
			s := strconv.Itoa(n)
			out = append(out, protoRule{iptV4: s, iptV6: s, nft: s, v4: true, v6: true})
		}
	}
	return out, nil
}

// ---- shared helpers ------------------------------------------------------

func portsCSV(ports []int) string    { return joinPorts(ports, ",") }
func portsSpaced(ports []int) string { return joinPorts(ports, ", ") }

func joinPorts(ports []int, sep string) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, sep)
}

// runIgnore runs a command and discards any error (used for idempotent deletes).
func runIgnore(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

// runStdin runs a command feeding it stdin (used for `nft -f -`).
func runStdin(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, out)
	}
	return nil
}
